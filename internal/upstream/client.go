// Package upstream isolates CLIProxyAPI executors and provider-specific protocols.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	exec "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator/builtin"
)

// Public Codex OAuth identifiers. Protocol headers are owned by this adapter.
const (
	clientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	redirectURI = "http://localhost:1455/auth/callback"
	// The catalog is version-gated: obsolete clients can receive HTTP 200 with only hidden models.
	// Keep discovery and its User-Agent aligned with a verified Codex client release.
	clientVersion = "0.155.1"
	// Keep response and event budgets independent of large client requests.
	MaxBody               = 8 << 20
	DefaultMaxRequestBody = 128 << 20
)

const DefaultCodexVersion = clientVersion

var (
	ErrInput        = errors.New("invalid_model_request")
	ErrContinuation = errors.New("continuation_requires_full_input")
	ErrUpstream     = errors.New("upstream_unavailable")
	ErrResponse     = errors.New("invalid_upstream_response")
)

type Client struct {
	engineOnce        sync.Once
	lifecycleMu       sync.Mutex
	closed            bool
	engineErr         error
	runtime           *sdkRuntime
	http              *http.Client
	inference         *http.Client
	tokenURL, baseURL string
	registry          *translator.Registry
	version           func() string
	grokVersion       func() string
	claudeVersion     func() string
	maxRequestBody    int64
}

type Options struct {
	Transport              http.RoundTripper
	Version                func() string
	GrokVersion            func() string
	ClaudeVersion          func() string
	MaxRequestBody         int64
	InferenceHeaderTimeout *time.Duration
}

func New() *Client {
	return NewWithVersion(nil)
}

// The resolver is installed before startup and returns an already validated version.
func NewWithVersion(version func() string) *Client {
	return NewWithOptions(Options{Version: version})
}

func (c *Client) codexVersion() string {
	if c.version != nil {
		if version := c.version(); version != "" {
			return version
		}
	}
	return DefaultCodexVersion
}

// NewWithTransport provides an HTTP seam for synthetic upstream tests. Production endpoints remain fixed.
func NewWithTransport(transport http.RoundTripper, version ...func() string) *Client {
	if transport == nil {
		transport = http.DefaultTransport
	}
	opts := Options{Transport: transport}
	if len(version) > 0 {
		opts.Version = version[0]
	}
	return NewWithOptions(opts)
}

func NewWithOptions(opts Options) *Client {
	transport := opts.Transport
	if transport == nil {
		base := http.DefaultTransport.(*http.Transport).Clone()
		base.ResponseHeaderTimeout = 20 * time.Second
		base.MaxIdleConns = 16
		base.MaxIdleConnsPerHost = 8
		transport = base
	}
	inferenceTransport := transport
	if base, ok := transport.(*http.Transport); ok {
		inference := base.Clone()
		inference.ResponseHeaderTimeout = 5 * time.Minute
		if opts.InferenceHeaderTimeout != nil {
			inference.ResponseHeaderTimeout = *opts.InferenceHeaderTimeout
		}
		inferenceTransport = inference
	}
	return &Client{
		http: &http.Client{
			Transport:     &proxyTransport{base: transport},
			CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		},
		inference:      &http.Client{Transport: &proxyTransport{base: inferenceTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		tokenURL:       "https://auth.openai.com/oauth/token",
		baseURL:        "https://chatgpt.com/backend-api/codex",
		registry:       builtin.Registry(),
		version:        opts.Version,
		grokVersion:    opts.GrokVersion,
		claudeVersion:  opts.ClaudeVersion,
		maxRequestBody: requestBodyLimit(opts.MaxRequestBody),
	}
}

func (c *Client) MaxRequestBody() int64 {
	return requestBodyLimit(c.maxRequestBody)
}

func requestBodyLimit(limits ...int64) int64 {
	if len(limits) > 0 && limits[0] > 0 {
		return limits[0]
	}
	return DefaultMaxRequestBody
}

func (c *Client) AuthorizationURL(state, challenge string) string {
	query := url.Values{"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirectURI}, "scope": {"openid email profile offline_access"}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "prompt": {"login"}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}}
	return "https://auth.openai.com/oauth/authorize?" + query.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (accounts.Credential, error) {
	return c.tokens(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}}, accounts.Credential{})
}

func (c *Client) Refresh(ctx context.Context, old accounts.Credential) (accounts.Credential, error) {
	if old.Kind() == "openai" {
		return accounts.Credential{}, accounts.ErrReauthorize
	}
	ctx = credentialContext(ctx, old)
	if old.Kind() != "codex" {
		// Refresh is also called after a 401, even when the advertised expiry is still in the future.
		next, err := c.providerTokens(ctx, old.Kind(), "", "", "", old)
		if err != nil {
			return accounts.Credential{}, err
		}

		if old.Kind() == "antigravity" {
			return c.prepareProject(ctx, next)
		}
		return next, nil
	}
	return c.tokens(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old.RefreshToken}}, old)
}

func (c *Client) tokens(ctx context.Context, form url.Values, old accounts.Credential) (accounts.Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	form.Set("client_id", clientID)
	request, err := http.NewRequestWithContext(ctx, "POST", c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return accounts.Credential{}, ErrInput
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return accounts.Credential{}, ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return accounts.Credential{}, tokenFailure(response)
	}
	raw, err := readBounded(response.Body, 128<<10)
	if err != nil {
		return accounts.Credential{}, ErrResponse
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(raw, &tokens) != nil || tokens.AccessToken == "" || tokens.ExpiresIn <= 0 || tokens.ExpiresIn > 366*24*3600 {
		return accounts.Credential{}, ErrResponse
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = old.RefreshToken
	}
	if tokens.IDToken == "" {
		tokens.IDToken = old.IDToken
	}
	normalized, _ := json.Marshal(accounts.Credential{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, IDToken: tokens.IDToken, AccountID: old.AccountID, Email: old.Email, Plan: old.Plan, ExpiresAt: time.Now().Unix() + tokens.ExpiresIn})
	credential, err := accounts.ParseCredential(normalized)
	if err != nil {
		return accounts.Credential{}, ErrResponse
	}
	return credential, nil
}

type Model struct {
	accounts.ModelCapabilities
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
	Hidden  bool   `json:"-"`
}

func (c *Client) Models(ctx context.Context, credential accounts.Credential) ([]Model, error) {
	value, err := c.Discover(ctx, credential)
	return slices.DeleteFunc(value.Models, func(model Model) bool { return model.Hidden }), err
}

type Discovery struct {
	Models []Model
	Source string
}

func (c *Client) Discover(ctx context.Context, credential accounts.Credential) (Discovery, error) {
	ctx = credentialContext(ctx, credential)
	version := c.codexVersion()
	models, err := c.models(ctx, credential, version)
	return Discovery{Models: models, Source: catalogSource(credential.Kind(), version)}, err
}

func (c *Client) models(ctx context.Context, credential accounts.Credential, version string) ([]Model, error) {
	if credential.Kind() != "codex" {
		return c.providerModels(ctx, credential)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models?client_version="+version, nil)
	if err != nil {
		return nil, ErrInput
	}
	upstreamHeaders(request, credential, nil, false, version)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, &UpstreamError{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	raw, err := readBounded(response.Body, 2<<20)
	if err != nil {
		return nil, ErrResponse
	}
	var catalog struct {
		Models []json.RawMessage `json:"models"`
	}
	if json.Unmarshal(raw, &catalog) != nil || catalog.Models == nil || len(catalog.Models) > 512 {
		return nil, ErrResponse
	}
	models := make([]Model, 0, len(catalog.Models))
	for _, raw := range catalog.Models {
		var model struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		}
		if json.Unmarshal(raw, &model) != nil {
			return nil, ErrResponse
		}
		if model.Slug != "" && len(model.Slug) <= 128 {
			models = append(models, Model{ModelCapabilities: discoveryCapabilities(raw, "codex"), ID: model.Slug, Object: "model", OwnedBy: "openai", Hidden: model.Visibility == "hide" || model.Visibility == "hidden"})
		}
	}
	return appendImageModels(models), nil
}

type UpstreamError struct {
	Status       int
	RetryAfter   string
	LimitedModel string
}

func (e *UpstreamError) Error() string { return "upstream_rejected_request" }

type Stream struct {
	*http.Response
	registry          *translator.Registry
	format            translator.Format
	model             string
	original, request []byte
	state             any
	messageUsage      messageUsage
	geminiUsage       geminiUsage
}

func (c *Client) Responses(ctx context.Context, credential accounts.Credential, raw []byte, headers http.Header, compact bool) (*Stream, error) {
	return c.execute(ctx, credential, raw, headers, translator.FormatOpenAIResponse, compact)
}

func (c *Client) Chat(ctx context.Context, credential accounts.Credential, raw []byte, headers http.Header) (*Stream, error) {
	return c.execute(ctx, credential, raw, headers, translator.FormatOpenAI, false)
}

func (c *Client) execute(ctx context.Context, credential accounts.Credential, raw []byte, headers http.Header, format translator.Format, compact bool) (*Stream, error) {
	ctx = credentialContext(ctx, credential)
	var input map[string]json.RawMessage
	if int64(len(raw)) > c.MaxRequestBody() || json.Unmarshal(raw, &input) != nil || input == nil {
		return nil, ErrInput
	}
	var model, previous string
	if json.Unmarshal(input["model"], &model) != nil || model == "" || len(model) > 128 {
		return nil, ErrInput
	}
	if previousRaw := input["previous_response_id"]; len(previousRaw) > 0 && string(previousRaw) != "null" {
		if json.Unmarshal(previousRaw, &previous) != nil {
			return nil, ErrInput
		}
		// The Codex HTTP backend does not preserve Responses state. WebSocket callers must reconstruct full input first.
		if previous != "" {
			return nil, ErrContinuation
		}
	}
	translated := c.registry.TranslateRequest(format, translator.FormatCodex, model, raw, true)
	var body map[string]json.RawMessage
	if json.Unmarshal(translated, &body) != nil {
		return nil, ErrInput
	}
	body["model"], _ = json.Marshal(model)
	if credential.Kind() == "openai" || credential.Kind() == "claude" && adaptiveClaudeModel(model) {
		// The intermediate Codex translator drops output limits; API and Claude limits must survive that hop.
		keys := []string{"max_output_tokens", "max_completion_tokens", "max_tokens"}
		for _, key := range keys {
			if value, ok := input[key]; ok && string(value) != "null" {
				var limit int64
				if json.Unmarshal(value, &limit) != nil || limit <= 0 {
					return nil, ErrInput
				}
				body["max_output_tokens"] = value
				break
			}
		}
	}
	body["stream"] = json.RawMessage("true")
	body["store"] = json.RawMessage("false")
	if _, ok := body["instructions"]; !ok {
		body["instructions"] = json.RawMessage(`""`)
	}
	for _, key := range []string{"previous_response_id", "prompt_cache_retention", "safety_identifier", "stream_options"} {
		delete(body, key)
	}
	if compact {
		delete(body, "stream")
		delete(body, "store")
	}
	encoded, err := json.Marshal(body)
	// Translation can expand the body beyond the limit already checked at ingress.
	if err != nil || int64(len(encoded)) > c.MaxRequestBody() {
		return nil, ErrInput
	}
	opts := exec.Options{Stream: !compact, SourceFormat: translator.FormatOpenAIResponse, ResponseFormat: translator.FormatOpenAIResponse}
	if compact {
		opts.Alt = "responses/compact"
	}
	response, err := c.runSDK(ctx, credential, encoded, headers, opts)
	if err != nil {
		return nil, err
	}
	return &Stream{Response: response, registry: c.registry, format: format, model: model, original: raw, request: encoded}, nil
}

func (s *Stream) Translate(ctx context.Context, event []byte) [][]byte {
	if s.format == translator.FormatClaude || s.format == translator.FormatGemini {
		return [][]byte{event}
	}
	return s.registry.TranslateStream(ctx, translator.FormatCodex, s.format, s.model, s.original, s.request, append([]byte("data: "), event...), &s.state)
}

func (s *Stream) Complete(ctx context.Context, event []byte) []byte {
	var terminal struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(event, &terminal) == nil && terminal.Type == "response.incomplete" {
		var value map[string]json.RawMessage
		if json.Unmarshal(event, &value) != nil {
			return nil
		}
		// The SDK converter recognizes completed events; preserve the actual response.status and incomplete_details.
		value["type"] = json.RawMessage(`"response.completed"`)
		event, _ = json.Marshal(value)
	}
	return s.registry.TranslateNonStream(ctx, translator.FormatCodex, s.format, s.model, s.original, s.request, event, &s.state)
}

func upstreamHeaders(request *http.Request, credential accounts.Credential, client http.Header, stream bool, version string) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("Chatgpt-Account-Id", credential.AccountID)
	applyCodexVersion(request.Header, version)
	request.Header.Set("Accept", "application/json")
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	}
	// Client session metadata is useful; client credentials, cookies and arbitrary proxy headers are never forwarded.
	for _, key := range []string{"X-Codex-Turn-Metadata", "X-Client-Request-Id", "Session_id", "X-Codex-Beta-Features"} {
		if value := client.Get(key); len(value) > 0 && len(value) <= 1024 {
			request.Header.Set(key, value)
		}
	}
}

func applyCodexVersion(headers http.Header, version string) {
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("User-Agent", "codex_cli_rs/"+version)
	headers.Set("Version", version)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrResponse
	}
	return raw, nil
}
