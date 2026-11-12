package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/oauth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

func TestGrokDeviceRoutesProtectSecretsAndSession(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := testdb.Open(ctx, filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	identity, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(db, key)
	client := upstream.NewWithTransport(gatewayTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/oauth2/device/code" {
			t.Error("unexpected device request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"device_code":"synthetic-private-device","user_code":"MOCK-CODE","verification_uri":"https://auth.x.ai/device","expires_in":600,"interval":5}`))}, nil
	}))
	defer client.Close()
	h := New(Options{Auth: identity, Accounts: service, OAuth: oauth.New(service, client), Ping: db.PingContext})
	origin := "http://example.test"
	setup := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-owner", "password": "synthetic-password", "workspace_name": "Synthetic workspace"}, nil)
	owner := setup.Result().Cookies()[0]
	begin := request(h, "POST", "/api/accounts/oauth", origin, map[string]string{"provider": "xai", "name": "Synthetic Grok"}, owner)
	if begin.Code != 201 || strings.Contains(begin.Body.String(), "synthetic-private-device") {
		t.Fatal("device begin failed or leaked secret", begin.Code)
	}
	var a oauth.Authorization
	if err := json.Unmarshal(begin.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	input := map[string]string{"state": a.State}
	poll := request(h, "POST", "/api/accounts/oauth/poll", origin, input, owner)
	if poll.Code != 200 || !strings.Contains(poll.Body.String(), `"interval":5`) {
		t.Fatal("device poll failed", poll.Code, poll.Body.String())
	}
	if r := request(h, "POST", "/api/accounts/oauth/poll", origin, input, nil); r.Code != 401 {
		t.Fatal("anonymous poll allowed", r.Code)
	}
	if r := request(h, "POST", "/api/accounts/oauth/poll", "https://other.example.test", input, owner); r.Code != 403 {
		t.Fatal("cross-origin poll allowed", r.Code)
	}
	if r := request(h, "DELETE", "/api/accounts/oauth/"+a.State, origin, map[string]string{}, owner); r.Code != 204 {
		t.Fatal("cancel failed", r.Code)
	}
	if r := request(h, "POST", "/api/accounts/oauth/poll", origin, input, owner); r.Code != 400 {
		t.Fatal("cancelled poll allowed", r.Code)
	}
}
