package server

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/updates"
)

type updateTransport struct{}

func (updateTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.3.0"}`))}, nil
}

func TestSoftwareUpdatesRequirePlatformOwnerAndSameOrigin(t *testing.T) {
	ctx := context.Background()
	connection, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "heartbeat"), []byte("ready"), 0644)
	service := updates.New("1.2.0", dir, updateTransport{})
	defer service.Close()
	h := New(Options{Auth: identity, Audit: audit.New(connection), Updates: service})
	origin := "http://example.test"
	owner := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-owner", "password": "synthetic-pass", "workspace_name": "Synthetic workspace"}, nil).Result().Cookies()[0]
	input := map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}
	if r := request(h, "POST", "/api/members", origin, input, owner); r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	member := request(h, "POST", "/api/auth/login", origin, input, nil).Result().Cookies()[0]
	path := "/api/settings/updates"
	for _, suffix := range []string{"", "/check", "/install", "/missing"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			if r := request(h, method, path+suffix, origin, map[string]any{}, nil); r.Code != 401 {
				t.Fatal("anonymous", method, suffix, r.Code)
			}
			if r := request(h, method, path+suffix, origin, map[string]any{}, member); r.Code != 403 {
				t.Fatal("member", method, suffix, r.Code)
			}
		}
	}
	if r := request(h, "GET", path, origin, nil, owner); r.Code != 200 || !strings.Contains(r.Body.String(), `"available":true`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(h, "POST", path+"/install", "https://foreign.example.test", map[string]any{}, owner); r.Code != 403 {
		t.Fatal("origin", r.Code)
	}
	if r := request(h, "POST", path+"/install", origin, map[string]any{"version": "1.3.0", "backup_confirmed": false}, owner); r.Code != 400 {
		t.Fatal("backup", r.Code, r.Body.String())
	}
	if r := request(h, "POST", path+"/install", origin, map[string]any{"version": "1.3.0", "backup_confirmed": true}, owner); r.Code != 202 {
		t.Fatal("install", r.Code, r.Body.String())
	}
	if r := request(h, "POST", path+"/install", origin, map[string]any{"version": "1.3.0", "backup_confirmed": true}, owner); r.Code != 409 {
		t.Fatal("duplicate", r.Code, r.Body.String())
	}
	if r := request(h, "DELETE", path, origin, nil, owner); r.Code != 405 {
		t.Fatal("method", r.Code)
	}
	if r := request(h, "GET", path+"/missing", origin, nil, owner); r.Code != 404 {
		t.Fatal("unknown", r.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "request.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec("UPDATE memberships SET role='admin' WHERE tenant_id=1 AND user_id=2"); err != nil {
		t.Fatal(err)
	}
	if r := request(h, "GET", path, origin, nil, member); r.Code != 403 {
		t.Fatal("workspace administrator is not platform owner", r.Code)
	}
	var audited int
	if err := connection.QueryRow("SELECT count(*) FROM audit_events WHERE action='updates.request'").Scan(&audited); err != nil || audited != 2 {
		t.Fatal("authorized update requests must be audited", audited, err)
	}
	second := New(Options{Auth: identity, TenantID: 2, Updates: service})
	if r := request(second, "GET", path, origin, nil, owner); r.Code != 401 {
		t.Fatal("workspace", r.Code)
	}
}
