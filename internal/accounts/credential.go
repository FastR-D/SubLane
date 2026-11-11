package accounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// Credential is internal secret material. HTTP responses must use Account instead.
type Credential struct {
	BaseURL      string                     `json:"base_url,omitempty"`
	APIPlatform  string                     `json:"api_platform,omitempty"`
	APIProtocol  string                     `json:"api_protocol,omitempty"`
	ProxyURL     string                     `json:"-"`
	Provider     string                     `json:"provider,omitempty"`
	Metadata     map[string]json.RawMessage `json:"metadata,omitempty"`
	AccessToken  string                     `json:"access_token"`
	RefreshToken string                     `json:"refresh_token"`
	IDToken      string                     `json:"id_token,omitempty"`
	AccountID    string                     `json:"account_id"`
	Email        string                     `json:"email,omitempty"`
	Plan         string                     `json:"plan,omitempty"`
	ExpiresAt    int64                      `json:"expires_at"`
	// RefreshAfter is owned by successful rotation, never trusted from imports.
	RefreshAfter int64 `json:"refresh_after,omitempty"`
	// Keep rejection with the encrypted token so a restart cannot make it eligible for fallback.
	Rejected bool `json:"access_token_rejected,omitempty"`
}

func (c Credential) Kind() string {
	if c.Provider == "" {
		return "codex"
	}
	return c.Provider
}

func ValidProvider(provider string) bool {
	return SubscriptionProvider(provider) || provider == "openai"
}

func SubscriptionProvider(provider string) bool {
	return provider == "codex" || provider == "claude" || provider == "antigravity" || provider == "xai"
}

func ParseCredential(raw []byte) (Credential, error) { return ParseFor("codex", raw) }

func ParseFor(provider string, raw []byte) (Credential, error) {
	if provider == "" {
		provider = "codex"
	}
	if !ValidProvider(provider) {
		return Credential{}, ErrInput
	}
	if provider == "openai" {
		return parseAPIKey(raw)
	}
	if provider != "codex" {
		return parseSubscription(provider, raw)
	}
	if len(raw) == 0 || len(raw) > 65536 {
		return Credential{}, ErrInput
	}
	var input struct {
		Credential
		Tokens  *Credential `json:"tokens"`
		Type    string      `json:"type"`
		Mode    string      `json:"auth_mode"`
		APIKey  string      `json:"OPENAI_API_KEY"`
		Expired string      `json:"expired"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Credential{}, ErrInput
	}
	if input.APIKey != "" || input.Type != "" && input.Type != "codex" || input.Mode != "" && input.Mode != "chatgpt" {
		return Credential{}, ErrInput
	}
	if input.Provider != "" && input.Provider != "codex" {
		return Credential{}, ErrInput
	}
	credential := input.Credential
	credential.Provider = ""
	credential.Metadata = nil
	if input.Tokens != nil {
		credential = *input.Tokens
		credential.Provider = ""
		credential.Metadata = nil
	}
	// Token claims describe upstream identity and routing, never SubLane authentication or roles.
	credential.BaseURL = ""
	credential.RefreshAfter = 0
	claims := tokenClaims(credential.IDToken)
	if claims.AccountID != "" {
		if credential.AccountID != "" && credential.AccountID != claims.AccountID {
			return Credential{}, ErrIdentity
		}
		credential.AccountID = claims.AccountID
	}
	if claims.Email != "" {
		credential.Email = claims.Email
	}
	if claims.Plan != "" {
		credential.Plan = claims.Plan
	}
	if credential.ExpiresAt == 0 {
		credential.ExpiresAt = tokenClaims(credential.AccessToken).Expiry
	}
	if credential.ExpiresAt == 0 && input.Expired != "" {
		expiry, err := time.Parse(time.RFC3339, input.Expired)
		if err != nil {
			return Credential{}, ErrInput
		}
		credential.ExpiresAt = expiry.Unix()
	}
	if err := credential.validate(); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (c Credential) validate() error {
	if !ValidProvider(c.Kind()) || !validToken(c.AccessToken) || (c.Kind() != "openai" && !validToken(c.RefreshToken)) || len(c.IDToken) > 16384 || c.AccountID == "" || len(c.AccountID) > 256 || len(c.Email) > 320 || len(c.Plan) > 64 || c.ExpiresAt < 0 {
		return ErrInput
	}
	if c.Kind() == "openai" {
		endpoint, err := normalizeBaseURL(c.BaseURL)
		if err != nil || endpoint != c.BaseURL || c.RefreshToken != "" || c.ExpiresAt != 0 || c.IDToken != "" || len(c.Metadata) != 0 {
			return ErrInput
		}
		if _, err := normalizeAPIConfig(c.BaseURL, c.APIPlatform, c.APIProtocol); err != nil {
			return err
		}
	} else if c.BaseURL != "" || c.APIPlatform != "" || c.APIProtocol != "" {
		return ErrInput
	}
	for _, value := range []string{c.AccountID, c.Email, c.Plan} {
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return ErrInput
		}
	}
	if c.Kind() == "codex" {
		id, access := tokenClaims(c.IDToken), tokenClaims(c.AccessToken)
		for _, token := range []claims{id, access} {
			for _, value := range []string{token.UserID, token.UserAlias, token.Subject} {
				if len(value) > 256 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
					return ErrInput
				}
			}
			if token.AccountID != "" && token.AccountID != c.AccountID {
				return ErrIdentity
			}
			if token.UserAlias != "" && token.UserAlias != token.UserID {
				return ErrIdentity
			}
		}
		if id.UserID != "" && access.UserID != "" && id.UserID != access.UserID {
			return ErrIdentity
		}
		if id.Subject != "" && access.Subject != "" && id.Subject != access.Subject {
			return ErrIdentity
		}
	}
	return nil
}

func validToken(value string) bool {
	return value != "" && len(value) <= 16384 && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

type claims struct {
	AccountID, UserID, UserAlias, Email, Plan, Subject string
	Expiry                                             int64
}

func tokenClaims(token string) claims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(raw) > 16384 {
		return claims{}
	}
	var payload struct {
		Email   string `json:"email"`
		Subject string `json:"sub"`
		Expiry  int64  `json:"exp"`
		Auth    struct {
			AccountID string `json:"chatgpt_account_id"`
			UserID    string `json:"chatgpt_user_id"`
			User      string `json:"user_id"`
			Plan      string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return claims{}
	}
	if payload.Auth.UserID == "" {
		payload.Auth.UserID = payload.Auth.User
	}
	return claims{Subject: payload.Subject, AccountID: payload.Auth.AccountID, UserID: payload.Auth.UserID, UserAlias: payload.Auth.User, Email: payload.Email, Plan: payload.Auth.Plan, Expiry: payload.Expiry}
}

// parseSubscription accepts the flat credential format exported by CLIProxyAPI. Only credential fields survive import.
func parseSubscription(provider string, raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 65536 {
		return Credential{}, ErrInput
	}
	var input struct {
		Credential
		Type             string `json:"type"`
		Expired          string `json:"expired"`
		APIKey           string `json:"api_key"`
		OpenAIKey        string `json:"OPENAI_API_KEY"`
		AccountUUID      string `json:"account_uuid"`
		OrganizationUUID string `json:"organization_uuid"`
		Subject          string `json:"sub"`
		AuthKind         string `json:"auth_kind"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Type != "" && input.Type != provider || input.Provider != "" && input.Provider != provider || input.APIKey != "" || input.OpenAIKey != "" {
		return Credential{}, ErrInput
	}
	if provider == "xai" && input.AuthKind != "" && input.AuthKind != "oauth" {
		return Credential{}, ErrInput
	}
	c := input.Credential
	c.RefreshAfter = 0
	c.BaseURL = ""
	c.Provider = provider
	c.Metadata = make(map[string]json.RawMessage)
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return Credential{}, ErrInput
	}
	for _, key := range []string{"account_uuid", "organization_uuid", "organization_name", "claude_device_ids", "project_id", "token_type"} {
		if value, ok := values[key]; ok {
			if key == "claude_device_ids" {
				var ids []string
				if json.Unmarshal(value, &ids) != nil || len(ids) > 16 {
					return Credential{}, ErrInput
				}
				for _, id := range ids {
					if !validMetadata(id) {
						return Credential{}, ErrInput
					}
				}
			} else {
				var text string
				if json.Unmarshal(value, &text) != nil || !validMetadata(text) {
					return Credential{}, ErrInput
				}
			}
			c.Metadata[key] = value
		}
	}
	c.AccountID = strings.ToLower(strings.TrimSpace(c.Email))
	if provider == "claude" && input.AccountUUID != "" {
		c.AccountID = input.AccountUUID
		if input.OrganizationUUID != "" {
			c.AccountID += ":" + input.OrganizationUUID
		}
	}
	if provider == "xai" {
		identity := tokenClaims(c.IDToken)
		if identity.Subject != "" {
			if input.Subject != "" && input.Subject != identity.Subject {
				return Credential{}, ErrIdentity
			}
			input.Subject = identity.Subject
		}
		if identity.Email != "" {
			c.Email = identity.Email
		}
		// The stable OAuth subject survives email changes and must agree with imported identity.
		if input.Subject != "" {
			if input.AccountID != "" && input.AccountID != input.Subject {
				return Credential{}, ErrIdentity
			}
			c.AccountID = input.Subject
		} else if input.AccountID != "" {
			c.AccountID = input.AccountID
		}
	}
	if input.Expired != "" {
		expiry, err := time.Parse(time.RFC3339, input.Expired)
		if err != nil {
			return Credential{}, ErrInput
		}
		c.ExpiresAt = expiry.Unix()
	}
	if err := c.validate(); err != nil {
		return Credential{}, err
	}
	return c, nil
}

func validMetadata(value string) bool {
	return len(value) <= 1024 && strings.IndexFunc(value, unicode.IsControl) < 0
}
