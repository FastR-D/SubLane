package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/content"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/vault"
)

func TestContentAdministrationRedactionOriginAndAudit(t *testing.T) {
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
	service := content.New(conn, v, 1)
	h := New(Options{Auth: identity, Content: service, Audit: audit.New(conn)})
	origin := "http://example.test"
	owner := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-owner", "password": "synthetic-password", "workspace_name": "Synthetic"}, nil).Result().Cookies()[0]
	if _, err = identity.CreateMember(ctx, "synthetic-member", "synthetic-password"); err != nil {
		t.Fatal(err)
	}
	member := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "synthetic-member", "password": "synthetic-password"}, nil).Result().Cookies()[0]
	for _, path := range []string{"/api/content", "/api/content/test", "/api/content/unknown"} {
		if got := request(h, "GET", path, "", nil, nil); got.Code != 401 {
			t.Fatal(got.Code)
		}
		if got := request(h, "GET", path, "", nil, member); got.Code != 403 {
			t.Fatal(got.Code)
		}
	}
	secret := "SYNTHETIC_SECRET_123"
	input := content.Input{Mode: "block", Rules: []content.RuleInput{{Name: "Synthetic", Kind: "text", Pattern: &secret, Enabled: true}}}
	if got := request(h, "PUT", "/api/content", "https://foreign.example.test", input, owner); got.Code != 403 {
		t.Fatal("lost origin protection", got.Code)
	}
	got := request(h, "PUT", "/api/content", origin, input, owner)
	if got.Code != 200 || strings.Contains(got.Body.String(), secret) {
		t.Fatal(got.Code, got.Body.String())
	}
	var saved content.State
	if err = json.Unmarshal(got.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	got = request(h, "POST", "/api/content/test", origin, map[string]any{"rule": content.RuleInput{ID: saved.Rules[0].ID, Kind: "text"}, "sample": secret}, owner)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"matched":true`) || strings.Contains(got.Body.String(), secret) {
		t.Fatal(got.Code, got.Body.String())
	}
	got = request(h, "PUT", "/api/content", origin, input, owner)
	if got.Code != 409 {
		t.Fatal("stale edit accepted", got.Code)
	}
	page, err := audit.New(conn).List(ctx, audit.Filter{Resource: "settings"})
	if err != nil || len(page.Events) != 1 || page.Events[0].ResourceID != "content" {
		t.Fatal(page, err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), secret) {
		t.Fatal("audit leaked pattern")
	}
}
