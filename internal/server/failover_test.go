package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestUnavailableConversationRebalancesAcrossHTTPAndWebSocket(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			type forwarded struct {
				auth  string
				input json.RawMessage
			}
			observed := make(chan forwarded, 2)
			f := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Input json.RawMessage `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				observed <- forwarded{r.Header.Get("Authorization"), body.Input}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_synthetic\",\"output\":[{\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Synthetic prior reply\"}]}]}}\n\n")
			})
			ctx := t.Context()
			rows, err := f.accounts.List(ctx)
			if err != nil || len(rows) != 1 {
				t.Fatal(err)
			}
			primary := rows[0].ID
			var conn *websocket.Conn
			if transport == "websocket" {
				conn, _, err = websocket.DefaultDialer.Dial(strings.Replace(f.server.URL, "http://", "ws://", 1)+"/v1/responses", http.Header{"Authorization": {"Bearer " + f.secret}, "Session_id": {"synthetic-failover"}})
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			}
			send := func(body string) {
				t.Helper()
				if conn != nil {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
						t.Fatal(err)
					}
					for {
						_, raw, err := conn.ReadMessage()
						if err != nil {
							t.Fatal(err)
						}
						var event struct {
							Type string `json:"type"`
						}
						json.Unmarshal(raw, &event)
						if event.Type == "error" {
							t.Fatal(string(raw))
						}
						if event.Type == "response.completed" {
							return
						}
					}
				}
				r, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/responses", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+f.secret)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Session_id", "synthetic-failover")
				response, err := http.DefaultClient.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != 200 || strings.Contains(string(raw), `"type":"error"`) {
					t.Fatal(response.StatusCode, string(raw), err)
				}
			}
			send(`{"type":"response.create","model":"synthetic-model","input":[{"role":"user","content":"Synthetic initial question"}]}`)
			<-observed
			backup, err := f.accounts.Authorize(ctx, "Synthetic backup", accounts.Credential{Provider: "codex", AccountID: "synthetic-backup", AccessToken: "synthetic-backup-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix()}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.accounts.SaveCatalog(ctx, backup.ID, 0, []string{"synthetic-model"}, time.Now().Unix(), upstream.CatalogSource("codex")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.groups.Save(ctx, 1, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{primary, backup.ID}}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.accounts.SetEnabled(ctx, primary, false); err != nil {
				t.Fatal(err)
			}
			if conn != nil {
				send(`{"type":"response.create","model":"synthetic-model","previous_response_id":"resp_synthetic","input":[{"role":"user","content":"Synthetic follow-up"}]}`)
			} else {
				send(`{"model":"synthetic-model","input":[{"role":"user","content":"Synthetic initial question"},{"role":"assistant","content":[{"type":"output_text","text":"Synthetic prior reply"}]},{"role":"user","content":"Synthetic follow-up"}]}`)
			}
			got := <-observed
			if got.auth != "Bearer synthetic-backup-access" {
				t.Fatal("disabled account reused", got.auth)
			}
			var input []json.RawMessage
			if err := json.Unmarshal(got.input, &input); err != nil || len(input) != 3 {
				t.Fatal("complete context not replayed", string(got.input), err)
			}
			for _, value := range []string{"Synthetic initial question", "Synthetic prior reply", "Synthetic follow-up"} {
				if !strings.Contains(string(got.input), value) {
					t.Fatal("context lost", value)
				}
			}
		})
	}
}
