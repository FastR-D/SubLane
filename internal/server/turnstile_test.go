package server

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/turnstile"
	"github.com/murongg/SubLane/internal/vault"
)

func TestTurnstileSettingsAndLoginBoundary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	conn, err := testdb.Open(ctx, filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	v, _ := vault.Open(filepath.Join(dir, "key"), true)
	identity, _ := auth.NewWithVault(conn, v)
	guard, err := turnstile.New(ctx, conn, v, &http.Client{Transport: turnstileTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Turnstile: guard, Tenants: tenants.New(conn), PublicURL: "https://example.test"})
	origin := "https://example.test"
	credentials := map[string]string{"username": "synthetic-admin", "password": "synthetic-password", "workspace_name": "Synthetic"}
	setup := request(h, "POST", "/api/auth/setup", origin, credentials, nil)
	if setup.Code != 201 {
		t.Fatal(setup.Code, setup.Body.String())
	}
	owner := setup.Result().Cookies()[0]
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	session, err := identity.Login(ctx, member.Username, "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	memberCookie := &http.Cookie{Name: sessionCookie, Value: session.Token}
	path := "/api/settings/turnstile"
	input := map[string]any{"enabled": true, "site_key": "synthetic-site", "secret_key": "synthetic-secret"}
	for _, method := range []string{"GET", "PATCH"} {
		if got := request(h, method, path, origin, input, nil); got.Code != 401 {
			t.Fatal("anonymous settings access", got.Code)
		}
		if got := request(h, method, path, origin, input, memberCookie); got.Code != 403 {
			t.Fatal("member settings access", got.Code)
		}
	}
	if got := request(h, "PATCH", path, "http://foreign.test", input, owner); got.Code != 403 {
		t.Fatal("origin bypass", got.Code)
	}
	if got := request(h, "PATCH", path, origin, input, owner); got.Code != 200 || strings.Contains(got.Body.String(), "synthetic-secret") {
		t.Fatal("save settings", got.Code, got.Body.String())
	}
	if got := request(h, "PATCH", path, origin, map[string]any{}, owner); got.Code != 400 || !guard.State().Enabled {
		t.Fatal("incomplete settings disabled login protection", got.Code)
	}
	other := New(Options{Auth: identity, Turnstile: guard, Tenants: tenants.New(conn), TenantID: 2})
	if got := request(other, "GET", path, origin, nil, owner); got.Code == 200 {
		t.Fatal("workspace bypass")
	}
	for _, handler := range []http.Handler{h, other} {
		state := request(handler, "GET", "/api/auth/state", "", nil, nil)
		if !strings.Contains(state.Body.String(), `"turnstile_site_key":"synthetic-site"`) || strings.Contains(state.Body.String(), "synthetic-secret") {
			t.Fatal("public configuration", state.Body.String())
		}
	}
	login := map[string]string{"username": "synthetic-admin", "password": "synthetic-password"}
	for _, path := range []string{"/api/auth/login", "/api/auth/passkey/begin"} {
		body := map[string]string{}
		if path == "/api/auth/login" {
			body = login
		}
		if got := request(h, "POST", path, origin, body, nil); got.Code != 403 || !strings.Contains(got.Body.String(), "turnstile_failed") || len(got.Result().Cookies()) != 0 {
			t.Fatal("captcha bypass", path, got.Code, got.Body.String())
		}
	}
	login["turnstile_token"] = "synthetic-token"
	if got := request(h, "POST", "/api/auth/login", origin, login, nil); got.Code != 200 || len(got.Result().Cookies()) != 1 {
		t.Fatal("verified login", got.Code, got.Body.String())
	}
	if got := request(h, "POST", "/api/auth/passkey/begin", origin, map[string]string{"turnstile_token": "synthetic-token"}, nil); got.Code != 200 {
		t.Fatal("verified passkey", got.Code, got.Body.String())
	}
	if got := request(h, "PATCH", path, origin, map[string]any{"enabled": false, "site_key": "synthetic-site"}, owner); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	delete(login, "turnstile_token")
	if got := request(h, "POST", "/api/auth/login", origin, login, nil); got.Code != 200 {
		t.Fatal("disabled login", got.Code)
	}
}

type turnstileTransport struct{}

func (turnstileTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"hostname":"example.test","action":"login"}`)), Header: make(http.Header)}, nil
}
