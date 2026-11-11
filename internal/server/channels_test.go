package server

import (
	"encoding/json"
	"github.com/murongg/SubLane/internal/upstream"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/groups"
)

func TestChannelManagementSeparatesSubscriptionsAndSecrets(t *testing.T) {
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider IO") })
	h := New(Options{Auth: f.identity, Accounts: f.accounts})
	origin := "http://example.test"
	login := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "owner-test", "password": "owner pass 42"}, nil)
	owner := login.Result().Cookies()[0]
	input := map[string]string{"name": "Synthetic channel", "api_key": "synthetic-channel-key", "base_url": "https://relay.example.test/v1"}
	created := request(h, "POST", "/api/channels", origin, input, owner)
	if created.Code != 201 {
		t.Fatal("channel creation", created.Code, created.Body.String())
	}
	var channel struct{ ID, Name, Protocol, BaseURL, Status string }
	var fields map[string]json.RawMessage
	if json.Unmarshal(created.Body.Bytes(), &fields) != nil {
		t.Fatal("channel response")
	}
	json.Unmarshal(created.Body.Bytes(), &channel)
	if channel.ID == "" || channel.Protocol != "openai" || channel.Status != "unverified" {
		t.Fatal("channel management model", created.Body.String())
	}
	for _, field := range []string{"email", "plan", "expires_at", "api_key", "access_token"} {
		if _, ok := fields[field]; ok {
			t.Fatal("subscription/secret field in channel", field)
		}
	}
	listed := request(h, "GET", "/api/channels", "", nil, owner)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), channel.ID) || strings.Contains(listed.Body.String(), "synthetic-channel-key") {
		t.Fatal("channel list", listed.Code, listed.Body.String())
	}
	subscriptions := request(h, "GET", "/api/accounts", "", nil, owner)
	if subscriptions.Code != 200 || strings.Contains(subscriptions.Body.String(), channel.ID) || !strings.Contains(subscriptions.Body.String(), "Synthetic subscription") {
		t.Fatal("channel leaked into subscription list", subscriptions.Body.String())
	}
	if result := request(h, "PATCH", "/api/channels/"+channel.ID, origin, map[string]bool{"enabled": false}, owner); result.Code != 200 {
		t.Fatal("channel disable", result.Code)
	}
	input["api_key"] = "synthetic-rotated-key"
	if result := request(h, "PUT", "/api/channels/"+channel.ID+"/key", origin, input, owner); result.Code != 200 || strings.Contains(result.Body.String(), "synthetic-rotated-key") {
		t.Fatal("channel rotation", result.Code, result.Body.String())
	}
	rows, _ := f.accounts.List(t.Context())
	for _, a := range rows {
		if a.Provider == "codex" {
			if result := request(h, "DELETE", "/api/channels/"+a.ID, origin, map[string]string{}, owner); result.Code != 404 {
				t.Fatal("subscription accepted as channel", result.Code)
			}
		}
	}
	if result := request(h, "GET", "/api/channels", "", nil, nil); result.Code != 401 {
		t.Fatal("anonymous channels", result.Code)
	}
	memberLogin := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "member-test", "password": "member pass 42"}, nil)
	member := memberLogin.Result().Cookies()[0]
	if result := request(h, "POST", "/api/channels", origin, input, member); result.Code != 403 {
		t.Fatal("member channel creation", result.Code)
	}
	if result := request(h, "POST", "/api/channels", "https://other.example.test", input, owner); result.Code != 403 {
		t.Fatal("cross-origin channel creation", result.Code)
	}
}

func TestChannelPlatformsPersistAcrossKeyRotation(t *testing.T) {
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider IO") })
	h := New(Options{Auth: f.identity, Accounts: f.accounts})
	origin := "http://example.test"
	login := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "owner-test", "password": "owner pass 42"}, nil)
	owner := login.Result().Cookies()[0]
	for _, tc := range []struct{ platform, protocol string }{{"anthropic", "anthropic"}, {"gemini", "gemini"}, {"custom", "responses"}} {
		t.Run(tc.platform, func(t *testing.T) {
			input := map[string]string{"name": "Synthetic " + tc.platform, "api_key": "synthetic-" + tc.platform + "-key", "base_url": "https://channel.example.test/v1", "platform": tc.platform, "protocol": tc.protocol}
			created := request(h, "POST", "/api/channels", origin, input, owner)
			var channel struct{ ID, Platform, Protocol string }
			if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &channel) != nil || channel.Platform != tc.platform || channel.Protocol != tc.protocol {
				t.Fatal("platform metadata", created.Code, created.Body.String())
			}
			delete(input, "platform")
			delete(input, "protocol")
			input["api_key"] += "-rotated"
			rotated := request(h, "PUT", "/api/channels/"+channel.ID+"/key", origin, input, owner)
			if rotated.Code != 200 || json.Unmarshal(rotated.Body.Bytes(), &channel) != nil || channel.Platform != tc.platform || channel.Protocol != tc.protocol {
				t.Fatal("rotation lost configuration", rotated.Code, rotated.Body.String())
			}
			stored, err := f.accounts.Prepare(t.Context(), channel.ID, nil)
			if err != nil || stored.APIConfig().Platform != tc.platform || stored.APIConfig().Protocol != tc.protocol {
				t.Fatal("persisted configuration", err)
			}
			input["platform"], input["protocol"] = "custom", "openai"
			if changed := request(h, "PUT", "/api/channels/"+channel.ID+"/key", origin, input, owner); changed.Code != 409 {
				t.Fatal("rotation rebound channel protocol", changed.Code, changed.Body.String())
			}
		})
	}
}

func TestOpenCodeChannelModelChecksAndConversationSessions(t *testing.T) {
	var sessions []string
	f := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/inference/go/v1/models" {
			io.WriteString(w, `{"data":[{"id":"synthetic-channel-model"}]}`)
			return
		}
		if r.URL.Path != "/inference/go/openai/v1/chat/completions" || r.Header.Get("X-Opencode-Session") == "" {
			t.Error("OpenCode compatibility missing", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		sessions = append(sessions, r.Header.Get("X-Opencode-Session"))
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"synthetic-chat\",\"object\":\"chat.completion.chunk\",\"model\":\"synthetic-channel-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Synthetic output\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	channel, err := f.accounts.ImportProvider(t.Context(), "openai", "Synthetic OpenCode", []byte(`{"api_key":"synthetic-opencode-key","base_url":"https://channel.example.test/inference/go/openai/v1","platform":"opencode-go"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.forwarding.Check(t.Context(), channel.ID); err != nil {
		t.Fatal(err)
	}
	if check, err := f.forwarding.CheckModel(t.Context(), channel.ID, "synthetic-channel-model"); err != nil || !check.Available {
		t.Fatal("OpenCode model test", check, err)
	}
	if _, err := f.groups.Save(t.Context(), 1, groups.Input{Name: "Synthetic API group", Enabled: true, AccountIDs: []string{channel.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"synthetic-session-a", "synthetic-session-a", "synthetic-session-b"} {
		req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/v1/responses", strings.NewReader(`{"model":"synthetic-channel-model","input":"Synthetic input"}`))
		req.Header.Set("Authorization", "Bearer "+f.secret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Opencode-Session", session)
		response, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), "Synthetic output") {
			t.Fatal("OpenCode gateway request", response.StatusCode, string(body), err)
		}
	}
	if len(sessions) != 4 || sessions[0] == sessions[1] || sessions[1] != sessions[2] || sessions[1] == sessions[3] || sessions[1] == "synthetic-session-a" {
		t.Fatal("OpenCode conversations were not scoped and stable", sessions)
	}
}

func TestChannelSettingsCanBeEditedInPlace(t *testing.T) {
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider IO") })
	h := New(Options{Auth: f.identity, Accounts: f.accounts, Groups: f.groups})
	origin := "http://example.test"
	login := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "owner-test", "password": "owner pass 42"}, nil)
	owner := login.Result().Cookies()[0]
	created := request(h, "POST", "/api/channels", origin, map[string]string{"name": "Synthetic original", "api_key": "synthetic-original-key", "base_url": "https://one.example.test/v1", "platform": "custom", "protocol": "openai"}, owner)
	var channel struct{ ID string }
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &channel) != nil {
		t.Fatal(created.Code, created.Body.String())
	}
	group := request(h, "POST", "/api/groups", origin, map[string]any{"name": "Synthetic channels", "enabled": true, "resources": []map[string]string{{"kind": "channel", "id": channel.ID}}}, owner)
	if group.Code != 201 {
		t.Fatal(group.Code, group.Body.String())
	}
	// A blank key must never carry the stored secret to another endpoint or protocol.
	for _, moved := range []map[string]string{
		{"name": "Synthetic original", "base_url": "https://attacker.example.test/v1", "platform": "custom", "protocol": "openai"},
		{"name": "Synthetic original", "base_url": "https://one.example.test/v1", "platform": "custom", "protocol": "responses"},
		{"name": "Synthetic original", "api_key": "synthetic-new-key", "base_url": "https://attacker.example.test/v1", "platform": "custom", "protocol": "openai"},
	} {
		if response := request(h, "PUT", "/api/channels/"+channel.ID, origin, moved, owner); response.Code != 409 {
			t.Fatal("endpoint or protocol change accepted", moved, response.Code, response.Body.String())
		}
	}
	stored, err := f.accounts.Prepare(t.Context(), channel.ID, nil)
	if err != nil || stored.AccessToken != "synthetic-original-key" || stored.BaseURL != "https://one.example.test/v1" || stored.APIConfig().Protocol != "openai" {
		t.Fatal("rejected edit changed the channel", err)
	}
	edited := request(h, "PUT", "/api/channels/"+channel.ID, origin, map[string]string{"name": "Synthetic renamed", "base_url": "https://one.example.test/v1", "platform": "custom", "protocol": "openai"}, owner)
	var result struct {
		ID, Name string
		BaseURL  string `json:"base_url"`
	}
	if edited.Code != 200 || json.Unmarshal(edited.Body.Bytes(), &result) != nil || result.ID != channel.ID || result.Name != "Synthetic renamed" || result.BaseURL != "https://one.example.test/v1" {
		t.Fatal("rename", edited.Code, edited.Body.String())
	}
	if strings.Contains(edited.Body.String(), "synthetic-original-key") {
		t.Fatal("edit response disclosed the key")
	}
	if stored, err := f.accounts.Prepare(t.Context(), channel.ID, nil); err != nil || stored.AccessToken != "synthetic-original-key" {
		t.Fatal("omitted key was not retained", err)
	}
	groupsList, err := f.groups.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range groupsList {
		if value.Name == "Synthetic channels" && value.ChannelCount != 1 {
			t.Fatal("edit removed group membership", value)
		}
	}
	if err := f.accounts.SaveCatalog(t.Context(), channel.ID, 0, []string{"synthetic-old-model"}, time.Now().Unix(), upstream.CatalogSource("openai")); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := f.connection.QueryRow(`SELECT models_revision FROM accounts WHERE id=?`, channel.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	rotated := request(h, "PUT", "/api/channels/"+channel.ID, origin, map[string]string{"name": "Synthetic renamed", "api_key": "synthetic-next-key", "base_url": "https://one.example.test/v1", "platform": "custom", "protocol": "openai"}, owner)
	if rotated.Code != 200 {
		t.Fatal(rotated.Code, rotated.Body.String())
	}
	// A replacement key must not reuse the previous key's model snapshot.
	var cleared bool
	var next int64
	if err := f.connection.QueryRow(`SELECT models_snapshot IS NULL, models_revision FROM accounts WHERE id=?`, channel.ID).Scan(&cleared, &next); err != nil || !cleared || next <= revision {
		t.Fatal("key replacement kept the old catalog", cleared, next, revision, err)
	}
	if stored, err := f.accounts.Prepare(t.Context(), channel.ID, nil); err != nil || stored.AccessToken != "synthetic-next-key" {
		t.Fatal("key was not replaced", err)
	}
	if response := request(h, "PUT", "/api/channels/"+channel.ID, origin, map[string]string{"name": "", "base_url": "https://one.example.test/v1"}, owner); response.Code != 400 {
		t.Fatal("invalid name accepted", response.Code, response.Body.String())
	}
}
