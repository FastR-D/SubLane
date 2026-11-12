package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/versions"
)

func TestClientVersionSettingsRequireAdminAndAuditChanges(t *testing.T) {
	for _, provider := range []versions.Provider{versions.Codex, versions.Grok, versions.Claude} {
		t.Run(string(provider), func(t *testing.T) {
			ctx := context.Background()
			connection, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "versions.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			identity, err := auth.New(connection)
			if err != nil {
				t.Fatal(err)
			}
			config, err := versions.NewForProvider(ctx, connection, provider, "0.100.0", func(context.Context) (string, error) { return "0.200.0", nil })
			if err != nil {
				t.Fatal(err)
			}
			defer config.Close()
			options := Options{Auth: identity, Audit: audit.New(connection)}
			if provider == versions.Codex {
				options.CodexVersions = config
			} else if provider == versions.Grok {
				options.GrokVersions = config
			} else {
				options.ClaudeVersions = config
			}
			h := New(options)
			origin := "http://example.test"
			setup := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-admin", "password": "synthetic-pass", "workspace_name": "Synthetic workspace"}, nil)
			admin := setup.Result().Cookies()[0]
			memberInput := map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}
			if result := request(h, "POST", "/api/members", origin, memberInput, admin); result.Code != 201 {
				t.Fatal(result.Code)
			}
			member := request(h, "POST", "/api/auth/login", origin, memberInput, nil).Result().Cookies()[0]
			path := "/api/settings/" + string(provider)
			if result := request(h, "DELETE", path, origin, nil, admin); result.Code != 405 || result.Header().Get("Content-Type") != "application/json; charset=utf-8" || result.Header().Get("Allow") != "GET, PATCH" {
				t.Fatal("settings method boundary", result.Code, result.Header(), result.Body.String())
			}
			if result := request(h, "GET", path, "", nil, nil); result.Code != 401 {
				t.Fatal("anonymous settings read", result.Code)
			}
			for _, method := range []string{"GET", "PATCH", "DELETE"} {
				if result := request(h, method, path, origin, map[string]any{}, member); result.Code != 403 {
					t.Fatal("member settings access", method, result.Code)
				}
			}
			input := map[string]any{"manual_version": "0.150.0", "auto_sync": false}
			if result := request(h, "PATCH", path, "https://foreign.example.test", input, admin); result.Code != 403 {
				t.Fatal("settings origin check", result.Code)
			}
			if result := request(h, "PATCH", path, origin, map[string]any{"manual_version": "0.150.0"}, admin); result.Code != 400 {
				t.Fatal("partial update silently disabled sync", result.Code)
			}
			result := request(h, "PATCH", path, origin, input, admin)
			if result.Code != 200 {
				t.Fatal("save", result.Code, result.Body.String())
			}
			var value versions.State
			if err := json.Unmarshal(result.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			if value.EffectiveVersion != "0.150.0" || value.AutoSync {
				t.Fatal(value)
			}
			if result := request(h, "POST", path+"/sync", origin, map[string]any{}, admin); result.Code != 200 {
				t.Fatal("manual sync", result.Code, result.Body.String())
			}
			if config.Current() != "0.150.0" || config.View().LatestVersion != "0.200.0" {
				t.Fatal("manual version overwritten")
			}
			if result := request(h, "POST", path+"/sync", origin, map[string]any{}, member); result.Code != 403 {
				t.Fatal("member requested sync", result.Code)
			}
			var events int
			if err := connection.QueryRow("SELECT count(*) FROM audit_events WHERE action='settings.update' AND outcome='success'").Scan(&events); err != nil || events != 1 {
				t.Fatal("save audit", events, err)
			}

		})
	}
}
