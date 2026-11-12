package server

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/alerts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/vault"
)

type alertTestSender func(*http.Request) (*http.Response, error)

func (f alertTestSender) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestWorkspaceAlertsRequireAdministratorAndNeverDiscloseURL(t *testing.T) {
	ctx := context.Background()
	conn, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	identity, err := auth.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(filepath.Join(t.TempDir(), "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	service := alerts.New(conn, v, alertTestSender(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	h := New(Options{Auth: identity, Alerts: service, Ping: conn.PingContext})
	origin := "http://example.test"
	owner := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-owner", "password": "synthetic-password", "workspace_name": "Synthetic"}, nil).Result().Cookies()[0]
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	_ = member
	login := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "synthetic-member", "password": "synthetic-password"}, nil).Result().Cookies()[0]
	for _, path := range []string{"/api/alerts", "/api/alerts/unknown", "/api/alerts/test"} {
		if got := request(h, "GET", path, "", nil, nil); got.Code != 401 {
			t.Fatal(got.Code)
		}
		if got := request(h, "GET", path, "", nil, login); got.Code != 403 {
			t.Fatal(got.Code)
		}
	}
	value := alerts.Input{Enabled: true, URL: "https://hooks.example.test/path?token=synthetic-secret"}
	if got := request(h, "PUT", "/api/alerts", "https://other.example.test", value, owner); got.Code != 403 {
		t.Fatal(got.Code)
	}
	got := request(h, "PUT", "/api/alerts", origin, value, owner)
	if got.Code != 200 || strings.Contains(got.Body.String(), "synthetic-secret") || !strings.Contains(got.Body.String(), "hooks.example.test") {
		t.Fatal(got.Code, got.Body.String())
	}
	if got := request(h, "POST", "/api/alerts/test", "https://foreign.example.test", map[string]any{}, owner); got.Code != 403 {
		t.Fatal("test lost origin protection", got.Code)
	}
	got = request(h, "POST", "/api/alerts/test", origin, map[string]any{}, owner)
	if got.Code != 200 || calls != 1 || !strings.Contains(got.Body.String(), `"delivered":true`) {
		t.Fatal(got.Code, got.Body.String(), calls)
	}
	got = request(h, "POST", "/api/alerts/test", origin, map[string]any{}, owner)
	if got.Code != 429 || calls != 1 || got.Header().Get("Retry-After") != "60" {
		t.Fatal("test cooldown", got.Code, calls)
	}
}
