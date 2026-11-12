package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestAuditBoundaryRedactionAndKeyUpdates(t *testing.T) {
	connection, err := testdb.Open(context.Background(), filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Keys: newTestKeyService(t, connection), Audit: audit.New(connection)})
	origin := "http://example.test"
	admin := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-admin", "password": "synthetic-pass", "workspace_name": "Synthetic workspace"}, nil).Result().Cookies()[0]
	request(h, "POST", "/api/members", origin, map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}, admin)
	member := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}, nil).Result().Cookies()[0]
	for _, cookie := range []*http.Cookie{nil, member} {
		code := request(h, "GET", "/api/audit", "", nil, cookie).Code
		if code != 401 && code != 403 {
			t.Fatal("audit exposed", code)
		}
	}
	configureTestPool(t, connection)
	created := request(h, "POST", "/api/keys", origin, map[string]any{"name": "Synthetic", "group_id": 1, "expires_at": time.Now().Add(time.Hour).Unix()}, member)
	var key apikey.CreatedKey
	if err := json.Unmarshal(created.Body.Bytes(), &key); err != nil || created.Code != 201 {
		t.Fatal("expiry creation", created.Body.String())
	}
	if got := request(h, "PATCH", "/api/keys/1", origin, map[string]any{"name": "Omitted expiry", "enabled": true}, member); got.Code != 400 {
		t.Fatal("omitted expiry silently removed deadline", got.Code)
	}
	updated := request(h, "PATCH", "/api/keys/1", origin, map[string]any{"name": "Renamed", "enabled": false, "expires_at": nil}, member)
	if updated.Code != 200 {
		t.Fatal("key update", updated.Body.String())
	}
	request(h, "PATCH", "/api/keys/1", origin, map[string]any{"name": "secret-body-that-must-not-be-logged", "enabled": true, "expires_at": nil}, admin)
	page := request(h, "GET", "/api/audit", "", nil, admin)
	if page.Code != 200 {
		t.Fatal("audit listing", page.Body.String())
	}
	var events audit.Page
	if err := json.Unmarshal(page.Body.Bytes(), &events); err != nil || len(events.Events) != 5 {
		t.Fatal("audit capture", page.Body.String(), err)
	}
	if events.Events[0].Outcome != "failure" || events.Events[0].HTTPStatus == nil || *events.Events[0].HTTPStatus != 404 {
		t.Fatal("failure attempt absent")
	}
	for _, secret := range []string{key.Secret, "synthetic-pass", "secret-body-that-must-not-be-logged", "token_hash"} {
		if strings.Contains(page.Body.String(), secret) {
			t.Fatal("audit leaked sensitive content")
		}
	}
	if request(h, "GET", "/api/audit?resource=unknown", "", nil, admin).Code != 400 {
		t.Fatal("invalid filter accepted")
	}
}

func TestProxyAuditTargets(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, check := range []struct{ method, path, action, resource, id string }{
		{http.MethodPost, "/api/proxies", "proxy.create", "proxy", ""},
		{http.MethodPost, "/api/proxies/import", "proxy.import", "proxy", ""},
		{http.MethodPost, "/api/proxies/prune", "proxy.prune", "proxy", ""},
		{http.MethodPut, "/api/proxies/" + id, "proxy.update", "proxy", id},
		{http.MethodDelete, "/api/proxies/" + id, "proxy.delete", "proxy", id},
		{http.MethodPut, "/api/accounts/" + id + "/proxy", "account.proxy", "account", id},
		{http.MethodPut, "/api/accounts/" + id + "/resets", "account.reset_settings", "account", id},
		{http.MethodPost, "/api/accounts/" + id + "/resets/consume", "account.reset_use", "account", id},
		{http.MethodPost, "/api/accounts/" + id + "/resets/retry", "account.reset_use", "account", id},
	} {
		action, resource, target := auditTarget(httptest.NewRequest(check.method, check.path, nil))
		if action != check.action || resource != check.resource || target != check.id {
			t.Fatalf("%s %s: %q %q %q", check.method, check.path, action, resource, target)
		}
	}
}
