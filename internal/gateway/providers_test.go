package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

func providerGateway(t *testing.T, transport http.RoundTripper) (*Service, map[string]string) {
	return providerFixture(t, transport, true)
}

func TestCodexOnlyPolicyExcludesStoredProvidersAndCatalogs(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected upstream request")
		return nil, errors.New("unexpected")
	}))
	ctx := context.Background()
	s.accounts.RestrictToCodex()
	if id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses); err != nil || id != ids["codex"] {
		t.Fatal("selected inactive provider", id, err)
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "", "claude", "synthetic-model", Responses); !errors.Is(err, accounts.ErrProviderDisabled) {
		t.Fatal("explicit inactive provider accepted", err)
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "legacy-session", "", "synthetic-model", Responses); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE account_affinity SET account_id = ? WHERE account_id = ?", ids["claude"], ids["codex"]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "legacy-session", "", "synthetic-model", Responses); !errors.Is(err, accounts.ErrProviderDisabled) {
		t.Fatal("inactive affinity accepted", err)
	}
	if _, err := s.AccountCatalog(ctx, ids["claude"], false); !errors.Is(err, accounts.ErrProviderDisabled) {
		t.Fatal("inactive catalog refreshed", err)
	}
	if _, err := s.Usage(ctx, ids["claude"]); !errors.Is(err, accounts.ErrProviderDisabled) {
		t.Fatal("inactive quota reader accepted", err)
	}
	group, err := s.GroupCatalog(ctx, 1, 1, false)
	if err != nil || len(group.Models) != 1 || group.KnownAccounts != 1 {
		t.Fatal("inactive model published", group, err)
	}
}

// Codex protocol fixtures must not accidentally execute their synthetic SSE through another provider.
func codexGateway(t *testing.T, transport http.RoundTripper) (*Service, map[string]string) {
	t.Helper()
	service, ids := providerGateway(t, transport)
	if _, err := groups.New(service.db).Save(context.Background(), 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"]}}); err != nil {
		t.Fatal(err)
	}
	return service, ids
}
func discoveryGateway(t *testing.T, transport http.RoundTripper) (*Service, map[string]string) {
	return providerFixture(t, transport, false)
}
func providerFixture(t *testing.T, transport http.RoundTripper, known bool, version ...func() string) (*Service, map[string]string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	connection, err := testdb.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(connection, cipher)
	ids := map[string]string{}
	for _, provider := range []string{"codex", "claude", "antigravity"} {
		row, err := service.Authorize(ctx, provider, accounts.Credential{Provider: provider, AccountID: "synthetic-subject", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix(), Metadata: map[string]json.RawMessage{"project_id": json.RawMessage(`"synthetic-project"`)}}, "")
		if err != nil {
			t.Fatal(err)
		}
		ids[provider] = row.ID
		if known {
			if err := service.SaveCatalog(ctx, row.ID, 0, []string{"synthetic-model"}, time.Now().Unix(), upstream.CatalogSource(provider)); err != nil {
				t.Fatal(err)
			}
		}
	}
	configureTestPool(t, connection)
	client := upstream.NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/oauth/usage" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"five_hour":null,"seven_day":null}`))}, nil
		}
		if r.URL.Host == "cloudcode-pa.googleapis.com" && r.URL.Path == "/v1internal:fetchAvailableModels" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":{}}`))}, nil
		}
		if r.URL.Path == "/backend-api/wham/usage" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{}}`))}, nil
		}
		if r.URL.Path == "/backend-api/wham/rate-limit-reset-credits" {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		return transport.RoundTrip(r)
	}), version...)
	t.Cleanup(client.Close)
	gateway := New(ctx, connection, service, client)
	gateway.policy.RetryDelay = time.Millisecond
	t.Cleanup(gateway.Close)
	return gateway, ids
}

func TestAffinityIsPartitionedByProviderAndPersists(t *testing.T) {
	gateway, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, errors.New("synthetic")
	}))
	ctx := context.Background()
	for range 2 {
		for provider, id := range ids {
			actual, _, err := gateway.selectAccount(ctx, 1, 1, "same-session", provider, "", Responses)
			if err != nil || actual != id {
				t.Fatal("provider binding crossed", provider, actual, err)
			}
		}
	}
	if _, err := gateway.accounts.SetEnabled(ctx, ids["claude"], false); err != nil {
		t.Fatal(err)
	}
	reopened := New(ctx, gateway.db, gateway.accounts, gateway.provider)
	defer reopened.Close()
	_, err := reopened.Open(ctx, 1, 1, []byte(`{"model":"claude/synthetic-model","input":"synthetic"}`), http.Header{"Session_id": {"same-session"}}, Responses)
	if !errors.Is(err, ErrNoAccount) {
		t.Fatal("disabled provider binding escaped its provider scope", err)
	}
	id, _, err := reopened.selectAccount(ctx, 1, 1, "same-session", "codex", "", Responses)
	if err != nil || id != ids["codex"] {
		t.Fatal("another provider affected", err)
	}
}

func TestModelCatalogKeepsHealthyProvidersWithNativeIDs(t *testing.T) {
	gateway, _ := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"models":[{"slug":"synthetic-codex"}]}`
		status := 200
		switch r.URL.Host {
		case "api.anthropic.com":
			status = 503
			body = `{}`
		case "daily-cloudcode-pa.googleapis.com":
			body = `{"models":{"synthetic-gemini":{}}}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	models, err := gateway.Models(context.Background(), 1, 1)
	if err != nil {
		t.Fatal("one provider hid every model", err)
	}
	got := []string{}
	for _, model := range models {
		got = append(got, model.ID)
	}
	if strings.Join(got, ",") != "synthetic-codex,synthetic-gemini" {
		t.Fatal("unexpected native model catalog", got)
	}
}

func TestFailedSubscriptionQuotaDoesNotVerifyOrPublishSnapshot(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"claude", "antigravity"} {
		f := newProviderQuotaFixture(t, kind)
		f.fail.Store(true)
		// A failed read must retain the import-equivalent, unverified state.
		if _, err := f.connection.Exec("UPDATE accounts SET status='unverified' WHERE id=?", f.id); err != nil {
			t.Fatal(err)
		}
		snapshot, err := f.service.Usage(ctx, f.id)
		if err == nil || snapshot.UpdatedAt != 0 {
			t.Fatal("failed quota read published a successful observation", kind, err)
		}
		row, err := f.accounts.Get(ctx, f.id)
		if err != nil || row.Status != "unverified" {
			t.Fatal("failed quota read verified account", err)
		}
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
