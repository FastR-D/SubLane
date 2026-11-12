package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/content"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

type gatewayTransport func(*http.Request) (*http.Response, error)

func (f gatewayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type forwardFixture struct {
	connection    *sql.DB
	upgrades      *atomic.Int32
	server        *httptest.Server
	secret        string
	keyID, userID int64
	keys          *apikey.Service
	identity      *auth.Service
	groups        *groups.Service
	accounts      *accounts.Service
	forwarding    *gateway.Service
	content       *content.Service
}

func newForwardFixture(t *testing.T, handler http.HandlerFunc) forwardFixture {
	return newProviderFixture(t, "codex", handler)
}
func newQuotaForwardFixture(t *testing.T, handler http.HandlerFunc) forwardFixture {
	return newForwardingFixture(t, "codex", handler, true)
}
func newProviderFixture(t *testing.T, provider string, handler http.HandlerFunc) forwardFixture {
	return newForwardingFixture(t, provider, handler, false)
}
func newForwardingFixture(t *testing.T, provider string, handler http.HandlerFunc, quota bool, maxRequestBody ...int64) forwardFixture {
	return newConfiguredForwardFixture(t, provider, handler, quota, gateway.DefaultPolicy(), maxRequestBody...)
}

func newConfiguredForwardFixture(t *testing.T, provider string, handler http.HandlerFunc, quota bool, policy gateway.Policy, maxRequestBody ...int64) forwardFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := testdb.Open(ctx, filepath.Join(dir, "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	identity, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "owner-test", "owner pass 42", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "member-test", "member pass 42")
	if err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(db, key)
	if _, err := service.Authorize(ctx, "Synthetic subscription", accounts.Credential{Provider: provider, Metadata: map[string]json.RawMessage{"project_id": json.RawMessage(`"synthetic-project"`)}, AccessToken: "synthetic-upstream-access", RefreshToken: "synthetic-upstream-refresh", AccountID: "synthetic-upstream-account", ExpiresAt: time.Now().Add(time.Hour).Unix()}, ""); err != nil {
		t.Fatal(err)
	}
	rows, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := service.SaveCatalog(ctx, row.ID, 0, []string{"synthetic-model"}, time.Now().Unix(), upstream.CatalogSource(provider)); err != nil {
			t.Fatal(err)
		}
	}
	fakeUpstream := httptest.NewServer(handler)
	t.Cleanup(fakeUpstream.Close)
	target, _ := url.Parse(fakeUpstream.URL)
	var bodyLimit int64
	if len(maxRequestBody) > 0 {
		bodyLimit = maxRequestBody[0]
	}
	client := upstream.NewWithOptions(upstream.Options{MaxRequestBody: bodyLimit, Transport: gatewayTransport(func(r *http.Request) (*http.Response, error) {
		if !quota && r.URL.Path == "/api/oauth/usage" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"five_hour":null,"seven_day":null}`))}, nil
		}
		if !quota && r.URL.Host == "cloudcode-pa.googleapis.com" && r.URL.Path == "/v1internal:fetchAvailableModels" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":{}}`))}, nil
		}
		if !quota && r.URL.Path == "/backend-api/wham/usage" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{}}`))}, nil
		}
		if !quota && r.URL.Path == "/backend-api/wham/rate-limit-reset-credits" {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		copy := r.Clone(r.Context())
		copy.URL.Scheme = target.Scheme
		copy.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(copy)
	})})
	t.Cleanup(client.Close)
	configureTestPool(t, db)
	keys := newTestKeyService(t, db)
	created, err := keys.CreateInGroup(ctx, member.ID, 1, "Synthetic client")
	if err != nil {
		t.Fatal(err)
	}
	forwarding := gateway.NewForTenantWithPolicy(ctx, db, service, client, 1, policy)
	rules := content.New(db, key, 1)
	forwarding.SetContent(rules)
	t.Cleanup(forwarding.Close)
	pools := groups.New(db)
	server := httptest.NewUnstartedServer(New(Options{Groups: pools, Auth: identity, Keys: keys, Accounts: service, Gateway: forwarding, Content: rules, Ping: db.PingContext}))
	upgrades := &atomic.Int32{}
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateHijacked {
			upgrades.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	return forwardFixture{connection: db, groups: pools, accounts: service, forwarding: forwarding, content: rules, server: server, upgrades: upgrades, secret: created.Secret, keyID: created.Key.ID, userID: member.ID, keys: keys, identity: identity}
}

func TestGatewayLargeRequests(t *testing.T) {
	prompt := strings.Repeat("x", (8<<20)+1)
	var calls atomic.Int32
	fixture := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !strings.Contains(string(body), prompt) {
			t.Error("large request content was not preserved", err)
		}
		calls.Add(1)
		if strings.HasSuffix(r.URL.Path, "/compact") {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"resp_test","output":[]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"output\":[]}}\n\n")
	})
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			body := []byte(`{"model":"synthetic-model","instructions":"` + prompt + `","input":[]}`)
			if path == "/v1/chat/completions" {
				body = []byte(`{"model":"synthetic-model","messages":[{"role":"user","content":"` + prompt + `"}]}`)
			}
			req, _ := http.NewRequest("POST", fixture.server.URL+path, strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+fixture.secret)
			req.Header.Set("Content-Type", "application/json")
			response, err := fixture.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("large request status: %d", response.StatusCode)
			}
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("websocket", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(fixture.server.URL, "http://", "ws://", 1)+"/v1/responses", http.Header{"Authorization": {"Bearer " + fixture.secret}})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		// Race instrumentation can take over 15 seconds to validate this 8 MiB body.
		conn.SetReadDeadline(time.Now().Add(time.Minute))
		if err := conn.WriteJSON(map[string]any{"type": "response.create", "model": "synthetic-model", "instructions": prompt, "input": []any{}}); err != nil {
			t.Fatal(err)
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type == "error" {
				t.Fatal("large websocket request was rejected")
			}
			if event.Type == "response.completed" {
				break
			}
		}
	})
	if calls.Load() != 4 {
		t.Fatalf("forwarded requests: %d, want 4", calls.Load())
	}
}

func TestGatewayConfiguredRequestBodyLimit(t *testing.T) {
	const limit = 1024
	var calls atomic.Int32
	fixture := newForwardingFixture(t, "codex", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}, false, limit)
	for _, chunked := range []bool{false, true} {
		for _, size := range []int{limit - 1, limit, limit + 1} {
			raw := `{"model":"synthetic-model","input":[]}`
			raw += strings.Repeat(" ", size-len(raw))
			req, _ := http.NewRequest("POST", fixture.server.URL+"/v1/responses", strings.NewReader(raw))
			if chunked {
				req.ContentLength = -1
			}
			req.Header.Set("Authorization", "Bearer "+fixture.secret)
			req.Header.Set("Content-Type", "application/json")
			response, err := fixture.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			want := http.StatusOK
			if size > limit {
				want = http.StatusRequestEntityTooLarge
			}
			if err != nil || response.StatusCode != want {
				t.Fatalf("size=%d chunked=%t: status=%d, want %d; %v", size, chunked, response.StatusCode, want, err)
			}
			if size > limit && !strings.Contains(string(body), "request_too_large") {
				t.Fatal("missing request limit error")
			}
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("oversized requests reached upstream: calls=%d", calls.Load())
	}
}

func TestGatewayForwardsResponsesAndDrainsCanceledStream(t *testing.T) {
	canceled := make(chan struct{}, 1)
	release := make(chan struct{})
	completedStream := make(chan struct{}, 1)
	fixture := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer synthetic-upstream-access" {
			t.Error("member key leaked upstream")
		}
		if strings.Contains(string(raw), "synthetic-rate") {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
			io.WriteString(w, `{"error":"synthetic-private-token"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n")
		w.(http.Flusher).Flush()
		if strings.Contains(string(raw), "synthetic-cancel") {
			select {
			case <-release:
				io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"output\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}}\n\n")
				completedStream <- struct{}{}
			case <-r.Context().Done():
				canceled <- struct{}{}
			}
			return
		}
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"output\":[]}}\n\n")
	})
	call := func(ctx context.Context, body string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, "POST", fixture.server.URL+"/v1/responses", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+fixture.secret)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := call(context.Background(), `{"model":"synthetic-model","input":"synthetic prompt"}`)
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatalf("forwarding status: %d", response.StatusCode)
	}
	var completed struct {
		ID string `json:"id"`
	}
	err := json.NewDecoder(response.Body).Decode(&completed)
	response.Body.Close()
	if err != nil || completed.ID != "resp_test" {
		t.Fatal("invalid completed response", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response = call(ctx, `{"model":"synthetic-model","input":"synthetic-cancel","stream":true}`)
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		response.Body.Close()
		t.Fatal("stream not started")
	}
	first := make([]byte, 64)
	if _, err := response.Body.Read(first); err != nil {
		t.Fatal(err)
	}
	cancel()
	response.Body.Close()
	select {
	case <-canceled:
		close(release)
		t.Fatal("upstream was canceled before final usage could arrive")
	default:
	}
	close(release)
	select {
	case <-completedStream:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not finish after disconnect")
	}
	deadline := time.After(3 * time.Second)
	for {
		page, err := fixture.forwarding.Requests(context.Background(), gateway.RequestFilter{})
		if err == nil {
			for _, record := range page.Requests {
				if record.InputTokens != nil && *record.InputTokens == 3 && record.OutputTokens != nil && *record.OutputTokens == 2 {
					goto usageRecorded
				}
			}
		}
		select {
		case <-deadline:
			t.Fatal("final usage was not recorded after disconnect")
		case <-time.After(10 * time.Millisecond):
		}
	}
usageRecorded:
	response = call(context.Background(), `{"model":"synthetic-model","input":"synthetic-rate"}`)
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 429 || response.Header.Get("Retry-After") != "2" || strings.Contains(string(data), "synthetic-private-token") {
		t.Fatal("upstream errors were not safely mapped")
	}

}

func TestGatewayTranslatesChatToolCalls(t *testing.T) {
	fixture := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Tools []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Tools) != 1 || input.Tools[0].Type != "function" || input.Tools[0].Name != "read_document" {
			t.Errorf("tool definition was lost during request translation: %+v, %v", input, err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tool\",\"model\":\"synthetic-model\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"id\":\"fc_synthetic\",\"call_id\":\"call_synthetic\",\"name\":\"read_document\",\"arguments\":\"{\\\"path\\\":\\\"synthetic.txt\\\"}\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n")
	})
	raw := `{"model":"synthetic-model","messages":[{"role":"user","content":"Read the synthetic document"}],"tools":[{"type":"function","function":{"name":"read_document","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`
	req, err := http.NewRequest("POST", fixture.server.URL+"/v1/chat/completions", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fixture.secret)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil || response.StatusCode != 200 || len(result.Choices) != 1 || len(result.Choices[0].Message.ToolCalls) != 1 {
		t.Fatal("tool response missing", response.StatusCode, err)
	}
	choice := result.Choices[0]
	tool := choice.Message.ToolCalls[0]
	if choice.FinishReason != "tool_calls" || tool.ID != "call_synthetic" || tool.Function.Name != "read_document" || tool.Function.Arguments != `{"path":"synthetic.txt"}` {
		t.Fatalf("tool call changed: %+v", choice)
	}
}
