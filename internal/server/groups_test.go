package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestGroupHTTPManagementAndPersonalChoices(t *testing.T) {
	connection, err := testdb.Open(context.Background(), filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Keys: newTestKeyService(t, connection), Groups: groups.New(connection), Ping: connection.PingContext})
	origin := "http://example.test"
	setup := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-admin", "password": "synthetic-pass", "workspace_name": "Synthetic workspace"}, nil)
	owner := setup.Result().Cookies()[0]
	configureTestPool(t, connection)
	status := request(h, "GET", "/api/system", "", nil, owner)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"has_usable_key":false`) {
		t.Fatalf("empty workspace key readiness: %d %s", status.Code, status.Body.String())
	}
	credentials := map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}
	created := request(h, "POST", "/api/members", origin, credentials, owner)
	var member auth.Member
	if err := json.Unmarshal(created.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}
	memberCookie := request(h, "POST", "/api/auth/login", origin, credentials, nil).Result().Cookies()[0]
	input := groups.Input{Name: "Private project", Enabled: true, AccountIDs: []string{}}
	if got := request(h, "POST", "/api/groups", origin, input, memberCookie).Code; got != 403 {
		t.Fatal("member managed groups", got)
	}
	if got := request(h, "POST", "/api/groups", "http://foreign.example.test", input, owner).Code; got != 403 {
		t.Fatal("cross-origin group edit", got)
	}
	response := request(h, "POST", "/api/groups", origin, input, owner)
	if response.Code != 201 {
		t.Fatal("create failed", response.Code, response.Body.String())
	}
	var pool groups.Detail
	if err := json.Unmarshal(response.Body.Bytes(), &pool); err != nil {
		t.Fatal(err)
	}
	choices := request(h, "GET", "/api/keys/groups", "", nil, memberCookie)
	if choices.Code != 200 || strings.Contains(choices.Body.String(), "Private project") {
		t.Fatal("private group leaked", choices.Body.String())
	}
	grantPath := fmt.Sprintf("/api/groups/members/%d", member.ID)
	if got := request(h, "PUT", grantPath, origin, map[string]any{"group_ids": []int64{pool.ID}}, memberCookie).Code; got != 403 {
		t.Fatal("member granted own access", got)
	}
	if got := request(h, "PUT", grantPath, origin, map[string]any{"group_ids": []int64{pool.ID}}, owner).Code; got != 200 {
		t.Fatal("grant failed", got)
	}
	poolMembersPath := fmt.Sprintf("/api/groups/%d/members", pool.ID)
	if got := request(h, "GET", poolMembersPath, "", nil, memberCookie).Code; got != 403 {
		t.Fatalf("member viewed pool grant roster: %d", got)
	}
	roster := request(h, "GET", poolMembersPath, "", nil, owner)
	if roster.Code != 200 || !strings.Contains(roster.Body.String(), `"username":"synthetic-member"`) {
		t.Fatalf("pool grant roster: %d %s", roster.Code, roster.Body.String())
	}
	choices = request(h, "GET", "/api/keys/groups?user_id=1", "", nil, memberCookie)
	if choices.Code != 200 || !strings.Contains(choices.Body.String(), "Private project") || !strings.Contains(choices.Body.String(), `"account_count":0`) || strings.Contains(choices.Body.String(), "Default") {
		t.Fatal("choices ignored member scope", choices.Body.String())
	}
	if got := request(h, "POST", "/api/keys", origin, map[string]any{"name": "Wrong pool", "group_id": 1}, memberCookie).Code; got != 403 {
		t.Fatal("forged group accepted", got)
	}
	if got := request(h, "POST", "/api/keys", origin, map[string]any{"name": "Project key", "group_id": pool.ID}, memberCookie).Code; got != 201 {
		t.Fatal("scoped key creation failed", got)
	}
	for _, path := range []string{"/api/groups", fmt.Sprintf("/api/groups/%d", pool.ID), grantPath} {
		if got := request(h, "GET", path, "", nil, memberCookie).Code; got != 403 {
			t.Fatal("member read management data", path, got)
		}
	}
	if got := request(h, "PATCH", "/api/groups/1", origin, groups.Input{Name: "Default", Enabled: false, AccountIDs: []string{}}, owner).Code; got != 200 {
		t.Fatal("ordinary pool cannot be disabled", got)
	}
}

func TestWebSocketRechecksPoolAndMemberAccessOnEveryTurn(t *testing.T) {
	for _, change := range []string{"grant", "disable", "account", "model", "prewarm-model", "paused"} {
		t.Run(change, func(t *testing.T) {
			var calls atomic.Int32
			fixture := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"synthetic-response\",\"output\":[]}}\n\n")
			})
			ctx := context.Background()
			accounts, err := fixture.accounts.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{accounts[0].ID}
			pool, err := fixture.groups.Save(ctx, 0, groups.Input{Name: "Private", Enabled: true, AccountIDs: ids})
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.groups.SetMemberGroups(ctx, fixture.userID, []int64{pool.ID}); err != nil {
				t.Fatal(err)
			}
			key, err := fixture.keys.CreateInGroup(ctx, fixture.userID, pool.ID, "Synthetic scoped key")
			if err != nil {
				t.Fatal(err)
			}
			conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(fixture.server.URL, "http://", "ws://", 1)+"/v1/responses", http.Header{"Authorization": {"Bearer " + key.Secret}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			read := func(want string) string {
				for {
					_, raw, err := conn.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					var event struct {
						Type string `json:"type"`
					}
					if json.Unmarshal(raw, &event) != nil {
						t.Fatal("invalid event")
					}
					if event.Type == want {
						return string(raw)
					}
					if event.Type == "error" {
						t.Fatal("unexpected error", string(raw))
					}
				}
			}
			turn := map[string]any{"type": "response.create", "model": "synthetic-model", "input": []any{}}
			if err := conn.WriteJSON(turn); err != nil {
				t.Fatal(err)
			}
			read("response.completed")
			want := "invalid_api_key"
			switch change {
			case "model", "prewarm-model":
				_, err = fixture.groups.Save(ctx, pool.ID, groups.Input{Name: pool.Name, Enabled: true, AccountIDs: ids, ModelPolicy: &groups.ModelPolicy{Restricted: true, Models: []string{"another-model"}}})
				want = "model_not_allowed"
			case "paused":
				_, err = fixture.keys.Update(ctx, fixture.userID, key.Key.ID, apikey.UpdateInput{Name: key.Key.Name, Enabled: false})
			case "grant":
				err = fixture.groups.SetMemberGroups(ctx, fixture.userID, []int64{})
			case "disable":
				_, err = fixture.groups.Save(ctx, pool.ID, groups.Input{Name: pool.Name, Enabled: false, AccountIDs: ids})
			case "account":
				_, err = fixture.groups.Save(ctx, pool.ID, groups.Input{Name: pool.Name, Enabled: true, AccountIDs: []string{}})
				want = "conversation_account_unavailable"
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "prewarm-model" {
				turn["generate"] = false
			}
			if err := conn.WriteJSON(turn); err != nil {
				t.Fatal(err)
			}
			if result := read("error"); !strings.Contains(result, want) || calls.Load() != 1 {
				t.Fatal("changed access admitted another turn", result, calls.Load())
			}
		})
	}
}

func TestSystemReadinessRequiresAnEnabledPoolWithAccounts(t *testing.T) {
	fixture := newForwardFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected upstream call") })
	h := fixture.server.Config.Handler
	login := request(h, "POST", "/api/auth/login", "http://example.test", map[string]string{"username": "owner-test", "password": "owner pass 42"}, nil)
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if _, err := fixture.groups.Save(context.Background(), 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	result := request(h, "GET", "/api/system", "", nil, cookie)
	var body struct {
		Gateway struct {
			Status string `json:"status"`
		} `json:"gateway"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &body) != nil || body.Gateway.Status != "not_configured" {
		t.Fatal("ungrouped account made gateway appear ready", result.Body.String())
	}
}
