package accounts

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/storage/db"
)

func parseAPIKey(raw []byte) (Credential, error) {
	var input struct {
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
		Platform string `json:"platform"`
		Protocol string `json:"protocol"`
	}
	if len(raw) == 0 || len(raw) > 65536 || json.Unmarshal(raw, &input) != nil || !validToken(input.APIKey) {
		return Credential{}, ErrInput
	}
	endpoint, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return Credential{}, err
	}
	config, err := normalizeAPIConfig(endpoint, input.Platform, input.Protocol)
	if err != nil {
		return Credential{}, err
	}
	// API keys have no durable upstream subject. A separate identity survives rotation without reserving old keys.
	credential := Credential{Provider: "openai", BaseURL: endpoint, AccessToken: input.APIKey, AccountID: rand.Text()}
	if input.Platform != "" || input.Protocol != "" {
		credential.APIPlatform, credential.APIProtocol = config.Platform, config.Protocol
	}
	return credential, nil
}

type APIConfig struct {
	Platform string
	Protocol string
}

// APIConfig preserves legacy Chat Completions channels while recognizing existing OpenCode endpoints.
func (c Credential) APIConfig() APIConfig {
	config, _ := normalizeAPIConfig(c.BaseURL, c.APIPlatform, c.APIProtocol)
	return config
}

func normalizeAPIConfig(endpoint, platform, protocol string) (APIConfig, error) {
	if platform == "" {
		platform = "custom"
		if u, err := url.Parse(endpoint); err == nil && strings.EqualFold(u.Hostname(), "opencode.ai") {
			platform = "opencode"
			if strings.HasPrefix(u.Path, "/zen/go/") || strings.HasPrefix(u.Path, "/inference/go/") {
				platform = "opencode-go"
			}
		}
	}
	switch platform {
	case "custom", "openai", "anthropic", "gemini", "deepseek", "openrouter", "opencode", "opencode-go":
	case "xai", "moonshot", "moonshot-cn", "zhipu", "zai", "minimax", "minimax-cn", "qwen", "qwen-cn", "mistral", "groq", "together", "fireworks", "siliconflow", "siliconflow-global", "nvidia", "modelscope":
	case "azure", "volcengine", "baidu", "tencent", "stepfun", "xiaomi":
	default:
		return APIConfig{}, ErrInput
	}
	if protocol == "" {
		protocol = "openai"
		if platform == "anthropic" || platform == "gemini" {
			protocol = platform
		}
	}
	switch protocol {
	case "openai", "responses", "anthropic", "gemini":
		return APIConfig{Platform: platform, Protocol: protocol}, nil
	default:
		return APIConfig{}, ErrInput
	}
}

func apiMetadata(account Account, credential Credential) Account {
	config := credential.APIConfig()
	account.BaseURL = credential.BaseURL
	account.APIPlatform, account.APIProtocol = config.Platform, config.Protocol
	return account
}

func normalizeBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 2048 || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", ErrInput
	}
	u, err := url.Parse(value)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", ErrInput
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." || strings.ContainsAny(segment, "\\\x00\r\n") {
			return "", ErrInput
		}
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

// APIChannelUpdate renames a channel or replaces its key. An empty APIKey keeps the stored key;
// BaseURL, Platform and Protocol must match the stored channel.
type APIChannelUpdate struct {
	Name     string
	APIKey   string
	BaseURL  string
	Platform string
	Protocol string
}

// UpdateAPIChannel changes the name or key without replacing the resource ID, group membership,
// limits or conversation bindings.
func (s *Service) UpdateAPIChannel(ctx context.Context, id string, input APIChannelUpdate) (Account, error) {
	name, err := NormalizeName(input.Name)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.get(ctx, id)
	if err != nil {
		return Account{}, err
	}
	if row.Provider != "openai" {
		return Account{}, ErrNotFound
	}
	previous, err := s.decrypt(row)
	if err != nil {
		return Account{}, err
	}
	key := input.APIKey
	if key == "" {
		key = previous.AccessToken
	}
	raw, err := json.Marshal(map[string]string{"api_key": key, "base_url": input.BaseURL, "platform": input.Platform, "protocol": input.Protocol})
	if err != nil {
		return Account{}, ErrInput
	}
	credential, err := parseAPIKey(raw)
	if err != nil {
		return Account{}, err
	}
	// The local upstream identity survives edits so duplicate checks and bindings stay attached to this channel.
	credential.AccountID, credential.ProxyURL = previous.AccountID, previous.ProxyURL
	if credential.APIPlatform == "" && credential.APIProtocol == "" {
		credential.APIPlatform, credential.APIProtocol = previous.APIPlatform, previous.APIProtocol
	}
	// Credentials and conversation bindings stay scoped to the original upstream: a stored key must
	// never be sent to another endpoint or protocol, so those changes still require a new channel.
	if previous.BaseURL != credential.BaseURL || previous.APIConfig() != credential.APIConfig() {
		return Account{}, ErrIdentity
	}
	connectionChanged := credential.AccessToken != previous.AccessToken
	status := row.Status
	if connectionChanged {
		status = "unverified"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if connectionChanged {
		if err := s.persist(ctx, q, id, credential, status); err != nil {
			return Account{}, err
		}
		// A new key may grant a different model set; discovery must not reuse the old snapshot.
		if err := q.InvalidateAccountCatalog(ctx, id); err != nil {
			return Account{}, err
		}
	}
	now := s.now().Unix()
	if name != row.Name {
		if err := q.RenameAccount(ctx, db.RenameAccountParams{ID: id, TenantID: s.tenantID, Name: name, UpdatedAt: now}); err != nil {
			return Account{}, err
		}
	}
	if err := audit.Record(ctx, q, "account.update", "account", id); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	delete(s.refreshes, id)
	row.Name, row.Status, row.UpdatedAt = name, status, now
	return apiMetadata(metadata(row), credential), nil
}
