package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/vault"
)

func TestAccountSummaryExcludesPausedProviders(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	connection, err := testdb.Open(ctx, filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-admin", "synthetic-password", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	legacy := accounts.New(connection, cipher)
	if _, err := legacy.Authorize(ctx, "Synthetic Claude", accounts.Credential{Provider: "claude", AccountID: "synthetic-claude", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, ""); err != nil {
		t.Fatal(err)
	}
	service := accounts.New(connection, cipher)
	service.RestrictToCodex()
	summary, err := accountSummary(ctx, service)
	if err != nil || summary["accounts_total"] != 1 || summary["accounts_enabled"] != 0 || summary["status"] != "not_configured" {
		t.Fatal("paused provider counted as ready", summary, err)
	}
}

func TestAccountManagementIsAdministratorOnlyAndNeverReturnsTokens(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := testdb.Open(ctx, filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	identity, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	accountService := accounts.New(db, cipher)
	h := New(Options{Auth: identity, Accounts: accountService, Ping: db.PingContext})
	origin := "http://example.test"
	setup := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "owner-test", "password": "owner pass 42", "workspace_name": "Synthetic workspace"}, nil)
	owner := setup.Result().Cookies()[0]
	input := map[string]string{"username": "member-test", "password": "member pass 42"}
	if result := request(h, "POST", "/api/members", origin, input, owner); result.Code != 201 {
		t.Fatal(result.Code)
	}
	login := request(h, "POST", "/api/auth/login", origin, input, nil)
	member := login.Result().Cookies()[0]
	imported := map[string]string{"name": "Synthetic subscription", "auth_json": `{"access_token":"synthetic-secret-access","refresh_token":"synthetic-secret-refresh","account_id":"upstream-test"}`}
	if result := request(h, "POST", "/api/accounts/import", origin, imported, member); result.Code != 403 {
		t.Fatal("member imported account")
	}
	if result := request(h, "GET", "/api/accounts", "", nil, nil); result.Code != 401 {
		t.Fatal("anonymous accounts listing")
	}
	created := request(h, "POST", "/api/accounts/import", origin, imported, owner)
	if created.Code != 201 {
		t.Fatalf("create: %d", created.Code)
	}
	var account accounts.Account
	if json.Unmarshal(created.Body.Bytes(), &account) != nil || account.ID == "" {
		t.Fatal("invalid metadata")
	}
	listed := request(h, "GET", "/api/accounts", "", nil, owner)
	if listed.Code != 200 || strings.Contains(listed.Body.String(), "synthetic-secret") || strings.Contains(listed.Body.String(), "refresh_token") {
		t.Fatal("account metadata leaked credentials")
	}
	if result := request(h, "PATCH", "/api/accounts/"+account.ID, origin, map[string]bool{"enabled": false}, owner); result.Code != 200 {
		t.Fatal("cannot disable")
	}
	if err := accountService.RefreshDue(ctx, account.ID, func(context.Context, accounts.Credential) (accounts.Credential, error) {
		return accounts.Credential{}, errors.New("synthetic-secret-upstream-body")
	}); !errors.Is(err, accounts.ErrRefresh) {
		t.Fatal("expected temporary refresh failure", err)
	}
	listed = request(h, "GET", "/api/accounts", "", nil, owner)
	var page struct {
		Accounts []accounts.Account `json:"accounts"`
	}
	if json.Unmarshal(listed.Body.Bytes(), &page) != nil || len(page.Accounts) != 1 || page.Accounts[0].Enabled || page.Accounts[0].CredentialRefresh == nil || !page.Accounts[0].CredentialRefresh.Failed || strings.Contains(listed.Body.String(), "synthetic-secret") {
		t.Fatal("missing safe disabled-account refresh diagnostics")
	}
	if result := request(h, "GET", "/api/accounts", "", nil, member); result.Code != 403 {
		t.Fatal("member read credential diagnostics")
	}
	if result := request(h, "DELETE", "/api/accounts/"+account.ID, origin, map[string]string{}, member); result.Code != 403 {
		t.Fatal("member deleted account")
	}
	if result := request(h, "DELETE", "/api/accounts/"+account.ID, origin, map[string]string{}, owner); result.Code != 204 {
		t.Fatalf("delete: %d", result.Code)
	}
	if result := request(h, "POST", "/api/accounts/import", "https://other.example.test", imported, owner); result.Code != 403 {
		t.Fatal("origin protection lost")
	}
}
