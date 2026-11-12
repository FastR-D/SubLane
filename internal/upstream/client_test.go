package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
)

func TestClientConfiguredRequestBodyLimit(t *testing.T) {
	var calls atomic.Int32
	raw := []byte(`{"model":"synthetic-model","input":[]}`)
	for _, limit := range []int64{int64(len(raw) - 1), int64(len(raw))} {
		client := NewWithOptions(Options{MaxRequestBody: limit, Transport: usageTransport(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("unexpected synthetic upstream call")
		})})
		if client.MaxRequestBody() != limit {
			t.Fatal("configured limit not applied")
		}
		_, err := client.Responses(context.Background(), accounts.Credential{AccessToken: "synthetic-access", AccountID: "synthetic-account"}, raw, nil, false)
		client.Close()
		if !errors.Is(err, ErrInput) {
			t.Fatal("raw or expanded request exceeded limit", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("oversized request reached upstream")
	}
}

func TestInferenceHeaderTimeoutIsIndependentOfMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		if r.URL.Path == "/models" {
			io.WriteString(w, `{"models":[{"slug":"synthetic-model","visibility":"list"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"synthetic\",\"output\":[]}}\n\n")
	}))
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 20 * time.Millisecond
	headerTimeout := 250 * time.Millisecond
	c := NewWithOptions(Options{Transport: transport, InferenceHeaderTimeout: &headerTimeout})
	defer c.Close()
	c.baseURL = server.URL
	credential := accounts.Credential{AccountID: "synthetic-subject", AccessToken: "synthetic-access"}
	if _, err := c.Models(context.Background(), credential); err == nil {
		t.Fatal("metadata timeout was widened")
	}
	stream, err := c.Responses(context.Background(), credential, []byte(`{"model":"synthetic-model","input":"synthetic"}`), nil, false)
	if err != nil {
		t.Fatalf("inference inherited metadata timeout: %v", err)
	}
	stream.Body.Close()
}

func TestOAuthExchangeAndRefreshUsePinnedEndpoints(t *testing.T) {
	idToken := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"member@example.test","https://api.openai.com/auth":{"chatgpt_account_id":"upstream-test","chatgpt_user_id":"synthetic-member","chatgpt_plan_type":"plus"}}`)) + ".synthetic"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/token" {
			t.Error("wrong token endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != clientID {
			t.Error("wrong public OAuth client")
		}
		if calls == 1 && (r.Form.Get("code_verifier") != "synthetic-verifier" || r.Form.Get("redirect_uri") != redirectURI) {
			t.Error("PKCE missing")
		}
		if calls == 2 && r.Form.Get("refresh_token") != "synthetic-refresh" {
			t.Error("wrong refresh token")
		}
		w.Header().Set("Content-Type", "application/json")
		tokens := map[string]any{"access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "expires_in": 3600}
		if calls == 1 {
			tokens["id_token"] = idToken
		}
		json.NewEncoder(w).Encode(tokens)
	}))
	defer upstream.Close()
	client := New()
	client.tokenURL = upstream.URL + "/token"
	started, _ := url.Parse(client.AuthorizationURL("synthetic-state", "synthetic-challenge"))
	if started.Host != "auth.openai.com" || started.Query().Get("code_challenge_method") != "S256" || started.Query().Get("state") != "synthetic-state" {
		t.Fatal("invalid authorization URL")
	}
	credential, err := client.Exchange(context.Background(), "synthetic-code", "synthetic-verifier")
	if err != nil || credential.AccountID != "upstream-test" || credential.Email != "member@example.test" || credential.ExpiresAt < time.Now().Unix() {
		t.Fatal("token exchange failed", err)
	}
	refreshed, err := client.Refresh(context.Background(), credential)
	if err != nil || refreshed.IDToken != idToken || refreshed.AccountID != "upstream-test" {
		t.Fatal("refresh lost member identity or upstream workspace routing", err)
	}
}

func TestSDKTranslationAndUpstreamHeaderIsolation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-upstream" || r.Header.Get("Chatgpt-Account-Id") != "upstream-test" {
			t.Error("upstream identity missing")
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Untrusted") != "" {
			t.Error("client headers leaked")
		}
		if r.URL.Path == "/models" {
			io.WriteString(w, `{"models":[{"slug":"synthetic-model","visibility":"list"}]}`)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["stream"] != true || body["store"] != false || body["model"] != "synthetic-model" {
			t.Error("SDK translation missing")
		}
		if _, ok := body["max_output_tokens"]; ok {
			t.Error("unsupported field forwarded")
		}
		if _, ok := body["previous_response_id"]; ok {
			t.Error("unsupported continuation forwarded")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_synthetic\",\"output\":[]}}\n\n")
	}))
	defer upstream.Close()
	client := New()
	client.baseURL = upstream.URL
	credential := accounts.Credential{AccessToken: "synthetic-upstream", AccountID: "upstream-test"}
	headers := http.Header{"Authorization": {"Bearer synthetic-member-key"}, "Cookie": {"synthetic-cookie"}, "X-Untrusted": {"synthetic"}}
	response, err := client.Responses(context.Background(), credential, []byte(`{"model":"synthetic-model","input":"synthetic prompt","max_output_tokens":50}`), headers, false)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("forwarding failed")
	}
	models, err := client.Models(context.Background(), credential)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-model" {
		t.Fatal("models not discovered", err)
	}
	if _, err := client.Responses(context.Background(), credential, []byte(`{"model":"synthetic-model","previous_response_id":"unknown-response","input":[]}`), nil, false); !errors.Is(err, ErrContinuation) {
		t.Fatal("stateless continuation silently dropped", err)
	}
}

func TestTokenErrorsDoNotExposeProviderBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":"invalid_grant","detail":"synthetic-secret"}`)
	}))
	defer upstream.Close()
	client := New()
	client.tokenURL = upstream.URL
	_, err := client.Refresh(context.Background(), accounts.Credential{RefreshToken: "synthetic-refresh"})
	if !errors.Is(err, accounts.ErrReauthorize) || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("unsafe provider error", err)
	}
}

func TestTokenRefreshRequiresExplicitCredentialFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		permanent bool
	}{
		{"invalid grant", 400, `{"error":"invalid_grant"}`, true},
		{"rotated token reused", 401, `{"error":{"code":"refresh_token_reused","message":"synthetic-private"}}`, true},
		{"invalidated token", 400, `{"error":{"code":"refresh_token_invalidated"}}`, true},
		{"client configuration", 400, `{"error":"invalid_client"}`, false},
		{"invalid scope", 400, `{"error":"invalid_scope"}`, false},
		{"unknown unauthorized", 401, `{"error":"synthetic_unknown","message":"invalid_grant"}`, false},
		{"proxy error", 400, `<html>synthetic-private</html>`, false},
		{"server error", 503, `{"error":"invalid_grant"}`, false},
		{"rate limit", 429, `{"error":"synthetic_limit"}`, false},
		{"oversized", 400, `{"error":"invalid_grant","detail":"` + strings.Repeat("x", 128<<10) + `"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewWithTransport(usageTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			defer client.Close()
			_, err := client.Refresh(context.Background(), accounts.Credential{RefreshToken: "synthetic-refresh"})
			if err == nil || errors.Is(err, accounts.ErrReauthorize) != tc.permanent || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatalf("wrong credential failure classification: %v", err)
			}
		})
	}
}

func TestTokenRefreshPreservesBoundedRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		value    string
		min, max int64
	}{
		{"90", 90, 90}, {"9999999", 3600, 3600}, {"-1", 0, 0}, {"invalid", 0, 0},
		{time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat), 115, 120},
	} {
		t.Run(tc.value, func(t *testing.T) {
			client := NewWithTransport(usageTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {tc.value}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}))
			defer client.Close()
			_, err := client.Refresh(context.Background(), accounts.Credential{RefreshToken: "synthetic-refresh"})
			var retry *accounts.RefreshError
			if !errors.As(err, &retry) || retry.RetryAfter < tc.min || retry.RetryAfter > tc.max {
				t.Fatalf("retry hint was lost or unbounded: %v", err)
			}
		})
	}
}

func TestModelsRequestsVersionGatedCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Error("unexpected catalog request")
		}
		w.Header().Set("Content-Type", "application/json")
		// Model discovery can succeed with only hidden entries for an obsolete client.
		if r.URL.Query().Get("client_version") != "0.155.1" || r.Header.Get("User-Agent") != "codex_cli_rs/0.155.1" {
			io.WriteString(w, `{"models":[{"slug":"synthetic-hidden","visibility":"hide"}]}`)
			return
		}
		io.WriteString(w, `{"models":[{"slug":"synthetic-current","visibility":"list"},{"slug":"synthetic-hidden","visibility":"hide"},{"slug":"synthetic-internal","visibility":"hidden"}]}`)
	}))
	defer server.Close()
	client := New()
	client.baseURL = server.URL
	models, err := client.Models(context.Background(), accounts.Credential{AccessToken: "synthetic-access", AccountID: "synthetic-account"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "synthetic-current" || models[0].Object != "model" || models[0].OwnedBy != "openai" {
		t.Fatalf("visible catalog missing or hidden models exposed: %+v", models)
	}
}

func TestDiscoveryRetainsHiddenModelCapabilities(t *testing.T) {
	client := NewWithTransport(usageTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-visible","visibility":"list"},{"slug":"codex-auto-review","visibility":"hide"},{"slug":"synthetic-hidden","visibility":"hidden"}]}`))}, nil
	}))
	defer client.Close()
	credential := accounts.Credential{AccessToken: "synthetic-access", AccountID: "synthetic-account"}
	discovery, err := client.Discover(context.Background(), credential)
	if err != nil || len(discovery.Models) != 3 {
		t.Fatalf("hidden capabilities discarded: %+v, %v", discovery, err)
	}
	models, err := client.Models(context.Background(), credential)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-visible" {
		t.Fatalf("hidden models entered the display catalog: %+v, %v", models, err)
	}
}

func TestCatalogModelIDPreservesSDKThinkingSuffixCompatibility(t *testing.T) {
	for _, value := range []string{"synthetic-model(high)", "synthetic-model(8192)", "synthetic-model(auto)"} {
		if actual := CatalogModelID(value); actual != "synthetic-model" {
			t.Fatalf("thinking option treated as another model: %s", actual)
		}
	}
	if CatalogModelID("synthetic-model") != "synthetic-model" || CatalogModelID("synthetic-model(high") != "synthetic-model(high" {
		t.Fatal("plain or incomplete name changed")
	}
}

func TestDiscoveryCapturesOneConfiguredVersion(t *testing.T) {
	var current atomic.Value
	current.Store("0.200.0")
	client := NewWithTransport(usageTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("client_version") != "0.200.0" || r.Header.Get("User-Agent") != "codex_cli_rs/0.200.0" || r.Header.Get("Version") != "0.200.0" {
			t.Error("request version disagrees across headers/query")
		}
		current.Store("0.300.0")
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-model"}]}`))}, nil
	}))
	client.version = func() string { return current.Load().(string) }
	value, err := client.Discover(context.Background(), accounts.Credential{AccessToken: "synthetic-access", AccountID: "synthetic-account"})
	if err != nil || value.Source != "codex:v4:0.200.0" || client.CatalogSource("codex") != "codex:v4:0.300.0" {
		t.Fatal("in-flight response was mislabeled with a newer version", value, err)
	}
}

func TestConfiguredCodexVersionReachesSDKForwarding(t *testing.T) {
	client := NewWithTransport(usageTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Version") != "0.200.0" || r.Header.Get("User-Agent") != "codex_cli_rs/0.200.0" || r.Header.Get("Originator") != "codex_cli_rs" {
			t.Error("SDK ignored configured version")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"synthetic\",\"output\":[]}}\n\n"))}, nil
	}))
	defer client.Close()
	client.version = func() string { return "0.200.0" }
	value, err := client.Responses(context.Background(), accounts.Credential{AccessToken: "synthetic-access", AccountID: "synthetic-account"}, []byte(`{"model":"synthetic-model","input":"synthetic"}`), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	value.Body.Close()
}
