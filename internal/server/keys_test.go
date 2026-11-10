package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/vault"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage"
)

func TestPersonalKeyOwnershipAndGatewayBoundary(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "key-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	identity, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Keys: newTestKeyService(t, db), Ping: db.PingContext, StartedAt: time.Now()})
	origin := "http://example.test"
	setup := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "owner-test", "password": "owner pass 42", "workspace_name": "Synthetic workspace"}, nil)
	owner := setup.Result().Cookies()[0]
	memberInput := map[string]string{"username": "member-test", "password": "member pass 42"}
	if got := request(h, "POST", "/api/members", origin, memberInput, owner).Code; got != 201 {
		t.Fatalf("member create: %d", got)
	}
	login := request(h, "POST", "/api/auth/login", origin, memberInput, nil)
	member := login.Result().Cookies()[0]
	configureTestPool(t, db)
	if result := request(h, "POST", "/api/keys", origin, map[string]string{"name": "Missing pool"}, member); result.Code != 400 {
		t.Fatalf("key created without choosing a pool: %d", result.Code)
	}
	created := request(h, "POST", "/api/keys", origin, map[string]any{"name": "Synthetic laptop", "group_id": 1}, member)
	if created.Code != 201 {
		t.Fatalf("member key create: %d", created.Code)
	}
	var key apikey.CreatedKey
	if err := json.Unmarshal(created.Body.Bytes(), &key); err != nil || key.Secret == "" {
		t.Fatal("missing creation secret")
	}
	list := request(h, "GET", "/api/keys", "", nil, member)
	if list.Code != 200 || strings.Contains(list.Body.String(), key.Secret) {
		t.Fatal("key listing exposed secret or denied owner")
	}
	if got := request(h, "POST", fmt.Sprintf("/api/keys/%d/revoke", key.Key.ID), origin, map[string]string{}, owner).Code; got != 404 {
		t.Fatalf("cross-user revocation: %d", got)
	}
	if got := request(h, "POST", "/api/keys", origin, map[string]any{"name": "Forged owner", "user_id": 1}, member).Code; got != 400 {
		t.Fatal("owner injection accepted")
	}
	gateway := func(path, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://example.test"+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := gateway("/v1/models", "", member).Code; got != 401 {
		t.Fatal("browser session used as gateway key")
	}
	if got := gateway("/api/system", key.Secret, nil).Code; got != 401 {
		t.Fatal("API key granted administrator access")
	}
	if got := gateway("/api/keys", key.Secret, nil).Code; got != 401 {
		t.Fatal("API key accessed key management")
	}
	result := gateway("/v1/models", key.Secret, nil)
	if result.Code != 501 || !strings.Contains(result.Body.String(), "gateway_not_configured") {
		t.Fatalf("gateway readiness: %d", result.Code)
	}
	if got := request(h, "POST", fmt.Sprintf("/api/keys/%d/revoke", key.Key.ID), origin, map[string]string{}, member).Code; got != 200 {
		t.Fatal("revoke failed")
	}
	if got := gateway("/v1/models", key.Secret, nil).Code; got != 401 {
		t.Fatal("revoked gateway key accepted")
	}
}

func newTestKeyService(t *testing.T, connection *sql.DB) *apikey.Service {
	t.Helper()
	cipher, err := vault.Open(filepath.Join(t.TempDir(), "credentials.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	return apikey.New(connection, cipher)
}

func TestKeyListFiltersRevokedBeforePagination(t *testing.T) {
	ctx := context.Background()
	connection, err := storage.Open(ctx, filepath.Join(t.TempDir(), "synthetic-keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	keys := newTestKeyService(t, connection)
	h := New(Options{Auth: identity, Keys: keys, Ping: connection.PingContext, StartedAt: time.Now()})
	setup := request(h, "POST", "/api/auth/setup", "http://example.test", map[string]string{
		"username": "synthetic-owner", "password": "synthetic-password", "workspace_name": "Synthetic workspace",
	}, nil)
	if setup.Code != 201 {
		t.Fatalf("setup: %d", setup.Code)
	}
	cookie := setup.Result().Cookies()[0]
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	configureTestPool(t, connection)
	create := func(userID int64, name string) apikey.CreatedKey {
		t.Helper()
		key, err := keys.CreateInGroup(ctx, userID, 1, name)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	paused := create(1, "Synthetic paused")
	if _, err := keys.Update(ctx, 1, paused.Key.ID, apikey.UpdateInput{Name: paused.Key.Name, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	expired := create(1, "Synthetic expired")
	if _, err := connection.Exec("UPDATE api_keys SET expires_at=1 WHERE id=?", expired.Key.ID); err != nil {
		t.Fatal(err)
	}
	active := create(1, "Synthetic active")
	foreign := create(member.ID, "Synthetic foreign")
	for i := range 60 {
		key := create(1, fmt.Sprintf("Synthetic revoked %d", i))
		if _, err := keys.Revoke(ctx, 1, key.Key.ID); err != nil {
			t.Fatal(err)
		}
	}
	list := func(path string) apikey.Page {
		t.Helper()
		result := request(h, "GET", path, "", nil, cookie)
		var page apikey.Page
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &page) != nil || page.Keys == nil {
			t.Fatalf("invalid key page: %d %s", result.Code, result.Body.String())
		}
		if strings.Contains(result.Body.String(), active.Secret) {
			t.Fatal("list disclosed a secret")
		}
		return page
	}
	for _, path := range []string{"/api/keys", "/api/keys?include_revoked=false"} {
		page := list(path)
		if len(page.Keys) != 3 || page.NextCursor != 0 || page.Keys[0].ID != active.Key.ID || page.Keys[1].ID != expired.Key.ID || page.Keys[2].ID != paused.Key.ID {
			t.Fatalf("revoked records displaced unrevoked keys: %+v", page)
		}
	}
	first := list("/api/keys?include_revoked=true")
	if len(first.Keys) != 50 || first.NextCursor != first.Keys[49].ID {
		t.Fatalf("invalid first page: %+v", first)
	}
	second := list(fmt.Sprintf("/api/keys?include_revoked=true&cursor=%d", first.NextCursor))
	if len(second.Keys) != 13 || second.NextCursor != 0 {
		t.Fatalf("invalid last page: %+v", second)
	}
	seen := map[int64]bool{}
	for _, key := range append(first.Keys, second.Keys...) {
		if seen[key.ID] || key.ID == foreign.Key.ID {
			t.Fatal("duplicate or another owner's key", key.ID)
		}
		seen[key.ID] = true
	}
	page := list(fmt.Sprintf("/api/keys?cursor=%d", active.Key.ID))
	if len(page.Keys) != 2 || page.Keys[0].ID != expired.Key.ID || page.NextCursor != 0 {
		t.Fatalf("unrevoked cursor ignored: %+v", page)
	}
	for _, key := range []apikey.CreatedKey{active, expired, paused} {
		if _, err := keys.Revoke(ctx, 1, key.Key.ID); err != nil {
			t.Fatal(err)
		}
	}
	if page := list("/api/keys"); len(page.Keys) != 0 || page.NextCursor != 0 {
		t.Fatalf("all-revoked list is not empty: %+v", page)
	}
	if page := list("/api/keys?include_revoked=true"); len(page.Keys) != 50 || page.NextCursor == 0 {
		t.Fatal("hidden records were lost")
	}
	if got := request(h, "GET", "/api/keys?include_revoked=invalid", "", nil, cookie).Code; got != 400 {
		t.Fatalf("invalid filter accepted: %d", got)
	}
}
func TestPersonalKeyDisclosureBoundaryAndNoStore(t *testing.T) {
	ctx := context.Background()
	connection, err := storage.Open(ctx, filepath.Join(t.TempDir(), "disclosure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	keys := newTestKeyService(t, connection)
	h := New(Options{Auth: identity, Keys: keys, Audit: audit.New(connection)})
	origin := "http://example.test"
	owner := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-admin", "password": "synthetic-pass", "workspace_name": "Synthetic workspace"}, nil).Result().Cookies()[0]
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	configureTestPool(t, connection)
	cookie := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}, nil).Result().Cookies()[0]
	created, err := keys.CreateInGroup(ctx, member.ID, 1, "Synthetic client")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/keys/%d/secret", created.Key.ID)
	for _, check := range []struct {
		method, origin string
		cookie         *http.Cookie
		status         int
	}{{"POST", origin, nil, 401}, {"POST", origin, owner, 404}, {"POST", "https://other.example.test", cookie, 403}, {"GET", "", cookie, 405}} {
		got := request(h, check.method, path, check.origin, map[string]any{}, check.cookie)
		if got.Code != check.status || strings.Contains(got.Body.String(), created.Secret) {
			t.Fatal("disclosure boundary", got.Code, check.status)
		}
	}
	r := httptest.NewRequest("POST", "http://example.test"+path, strings.NewReader("{}"))
	r.Header.Set("Origin", origin)
	r.Header.Set("Authorization", "Bearer "+created.Secret)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("bearer key used for secret disclosure")
	}
	got := request(h, "POST", path, origin, map[string]any{}, cookie)
	if got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("disclosure failed or cacheable", got.Code)
	}
	var result struct {
		Secret string `json:"secret"`
	}
	if json.Unmarshal(got.Body.Bytes(), &result) != nil || result.Secret != created.Secret {
		t.Fatal("wrong key returned")
	}
	if strings.Contains(request(h, "GET", "/api/keys", "", nil, cookie).Body.String(), created.Secret) {
		t.Fatal("list leaked secret")
	}
	events, err := audit.New(connection).List(ctx, audit.Filter{})
	if err != nil || events.Events[0].Action != "key.reveal" || events.Events[0].Outcome != "success" {
		t.Fatal("missing disclosure audit", err)
	}
	if _, err := keys.Revoke(ctx, member.ID, created.Key.ID); err != nil {
		t.Fatal(err)
	}
	if got := request(h, "POST", path, origin, map[string]any{}, cookie); got.Code != 409 || strings.Contains(got.Body.String(), created.Secret) {
		t.Fatal("revoked key disclosed")
	}
}

func configureTestPool(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"INSERT OR IGNORE INTO account_groups(id,name,enabled,created_at,updated_at) VALUES(1,'Default',1,1,1)",
		"INSERT OR IGNORE INTO group_accounts(group_id,account_id) SELECT 1,id FROM accounts",
		"INSERT OR IGNORE INTO group_members SELECT 1,id FROM users",
	} {
		if _, err := conn.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
