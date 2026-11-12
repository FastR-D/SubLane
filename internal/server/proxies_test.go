package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

func TestProxyManagementAndBindingAreAdministratorOnly(t *testing.T) {
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
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(connection, cipher)
	handler := New(Options{Auth: identity, Accounts: service, Ping: connection.PingContext})
	origin := "http://example.test"
	setup := request(handler, "POST", "/api/auth/setup", origin, map[string]string{"username": "owner-test", "password": "owner pass 42", "workspace_name": "Synthetic workspace"}, nil)
	owner := setup.Result().Cookies()[0]
	if result := request(handler, "POST", "/api/members", origin, map[string]string{"username": "member-test", "password": "member pass 42"}, owner); result.Code != 201 {
		t.Fatal(result.Code)
	}
	login := request(handler, "POST", "/api/auth/login", origin, map[string]string{"username": "member-test", "password": "member pass 42"}, nil)
	member := login.Result().Cookies()[0]
	address := "http://synthetic-user:synthetic-pass@127.0.0.1:18080"
	input := map[string]string{"name": "Synthetic exit", "url": address}
	if result := request(handler, "POST", "/api/proxies", origin, input, member); result.Code != http.StatusForbidden {
		t.Fatal("member created proxy", result.Code)
	}
	created := request(handler, "POST", "/api/proxies", origin, input, owner)
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), "synthetic-pass") {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var proxy accounts.Proxy
	if err := json.Unmarshal(created.Body.Bytes(), &proxy); err != nil || proxy.ID == "" {
		t.Fatalf("proxy: %+v %v", proxy, err)
	}
	if result := request(handler, "GET", "/api/proxies", "", nil, member); result.Code != http.StatusForbidden {
		t.Fatal("member listed proxies", result.Code)
	}
	if result := request(handler, "POST", "/api/proxies/prune", origin, map[string]string{}, member); result.Code != http.StatusForbidden {
		t.Fatal("member pruned proxies", result.Code)
	}
	listed := request(handler, "GET", "/api/proxies", "", nil, owner)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "synthetic-pass") {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	imported := request(handler, "POST", "/api/accounts/import", origin, map[string]string{"name": "Synthetic account", "auth_json": `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","account_id":"synthetic-account"}`, "proxy_id": proxy.ID}, owner)
	if imported.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", imported.Code, imported.Body.String())
	}
	var account accounts.Account
	if err := json.Unmarshal(imported.Body.Bytes(), &account); err != nil || account.ProxyID != proxy.ID {
		t.Fatalf("account: %+v %v", account, err)
	}
	if result := request(handler, "DELETE", "/api/proxies/"+proxy.ID, origin, map[string]string{}, owner); result.Code != http.StatusConflict {
		t.Fatal("deleted bound proxy", result.Code)
	}
	if result := request(handler, "PUT", "/api/accounts/"+account.ID+"/proxy", origin, map[string]string{"proxy_id": ""}, member); result.Code != http.StatusForbidden {
		t.Fatal("member unbound account", result.Code)
	}
	if result := request(handler, "PUT", "/api/accounts/"+account.ID+"/proxy", origin, map[string]string{"proxy_id": ""}, owner); result.Code != http.StatusOK {
		t.Fatal("cannot unbind", result.Code)
	}
	if result := request(handler, "DELETE", "/api/proxies/"+proxy.ID, origin, map[string]string{}, owner); result.Code != http.StatusNoContent {
		t.Fatal("cannot delete unbound proxy", result.Code)
	}
}

func TestProxyBulkImportAndLocationCheck(t *testing.T) {
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
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(connection, cipher)
	seen := ""
	handler := New(Options{Auth: identity, Accounts: service, Ping: connection.PingContext, ProxyCheck: func(_ context.Context, address string) (upstream.ProxyCheck, error) {
		seen = address
		if strings.Contains(address, "19090") {
			return upstream.ProxyCheck{ErrorCode: "connection_failed"}, nil
		}
		return upstream.ProxyCheck{Reachable: true, ExitIP: "203.0.113.8", Country: "US", Region: "Test region", City: "Test city", LatencyMS: 42}, nil
	}})
	origin := "http://example.test"
	owner := request(handler, "POST", "/api/auth/setup", origin, map[string]string{"username": "owner-test", "password": "owner pass 42", "workspace_name": "Synthetic workspace"}, nil).Result().Cookies()[0]
	input := map[string]string{"text": "http://synthetic-user:synthetic-pass@127.0.0.1:18080\nNamed exit | socks5://127.0.0.1:19090"}
	created := request(handler, "POST", "/api/proxies/import", origin, input, owner)
	if created.Code != 201 || strings.Contains(created.Body.String(), "synthetic-pass") {
		t.Fatalf("import: %d %s", created.Code, created.Body.String())
	}
	var result struct {
		Proxies []accounts.Proxy `json:"proxies"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || len(result.Proxies) != 2 {
		t.Fatalf("bulk result: %+v %v", result, err)
	}
	checked := request(handler, "POST", "/api/proxies/"+result.Proxies[0].ID+"/check", origin, map[string]string{}, owner)
	var proxy accounts.Proxy
	if err := json.Unmarshal(checked.Body.Bytes(), &proxy); checked.Code != 200 || err != nil || proxy.ExitIP != "203.0.113.8" || proxy.Country != "US" || seen != "http://synthetic-user:synthetic-pass@127.0.0.1:18080" {
		t.Fatalf("check: %d %+v %v", checked.Code, proxy, err)
	}
	listed := request(handler, "GET", "/api/proxies", "", nil, owner)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), "Test city") || strings.Contains(listed.Body.String(), "synthetic-pass") {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	if check := request(handler, "POST", "/api/proxies/"+result.Proxies[1].ID+"/check", origin, map[string]string{}, owner); check.Code != 200 {
		t.Fatalf("failed proxy check: %d %s", check.Code, check.Body.String())
	}
	pruned := request(handler, "POST", "/api/proxies/prune", origin, map[string]string{}, owner)
	var count struct {
		DeletedCount int64 `json:"deleted_count"`
	}
	if err := json.Unmarshal(pruned.Body.Bytes(), &count); pruned.Code != 200 || err != nil || count.DeletedCount != 1 {
		t.Fatalf("prune: %d %+v %v", pruned.Code, count, err)
	}
}
