package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/pricing"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/testdb"
)

func pricingFixture(t *testing.T, catalog *pricing.Service) (http.Handler, *http.Cookie, *http.Cookie) {
	t.Helper()
	connection, err := testdb.Open(context.Background(), filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Auth: identity, Tenants: tenants.New(connection), Pricing: catalog})
	setup := request(handler, "POST", "/api/auth/setup", "http://example.test", map[string]string{
		"username": "synthetic-owner", "password": "synthetic pass 42", "workspace_name": "Synthetic workspace",
	}, nil)
	if setup.Code != 201 {
		t.Fatal("setup", setup.Code, setup.Body.String())
	}
	owner := setup.Result().Cookies()[0]
	if _, err := identity.CreateMember(context.Background(), "synthetic-member", "synthetic pass 42"); err != nil {
		t.Fatal(err)
	}
	login := request(handler, "POST", "/api/auth/login", "http://example.test", map[string]string{
		"username": "synthetic-member", "password": "synthetic pass 42",
	}, nil)
	if login.Code != 200 {
		t.Fatal("login", login.Code, login.Body.String())
	}
	return handler, owner, login.Result().Cookies()[0]
}

func TestPricingCatalogRequiresSessionAndAllowsBothRoles(t *testing.T) {
	catalog := pricing.NewStatic(map[string]pricing.Price{
		"synthetic-model": {Input: 1250000, Cached: 125000, Output: 10000000, Source: "override"},
	})
	handler, owner, member := pricingFixture(t, catalog)
	for _, cookie := range []*http.Cookie{owner, member} {
		result := request(handler, "GET", "/api/pricing", "", nil, cookie)
		var body struct {
			Unit   string `json:"unit"`
			Prices []struct {
				Model string `json:"model"`
				pricing.Price
			} `json:"prices"`
		}
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &body) != nil || body.Unit != "micro_usd_per_million_tokens" || len(body.Prices) != 1 {
			t.Fatal("price catalog", result.Code, result.Body.String())
		}
		if got := body.Prices[0]; got.Model != "synthetic-model" || got.Input != 1250000 || got.Cached != 125000 || got.Output != 10000000 || got.Source != "override" {
			t.Fatalf("unexpected price: %+v", got)
		}
		if result.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("pricing response was cacheable")
		}
		if result := request(handler, "POST", "/api/pricing", "http://example.test", nil, cookie); result.Code != 405 {
			t.Fatal("catalog must be read-only", result.Code)
		}
	}
	for _, method := range []string{"GET", "POST"} {
		if result := request(handler, method, "/api/pricing", "http://example.test", nil, nil); result.Code != 401 {
			t.Fatal("anonymous catalog access", method, result.Code)
		}
	}
	if result := request(handler, "GET", "/api/pricing/missing", "", nil, member); result.Code != 403 {
		t.Fatal("unknown path escaped default administrator boundary", result.Code)
	}
}

func TestPricingCatalogDistinguishesEmptyFromUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		catalog *pricing.Service
		status  int
	}{
		{"empty", pricing.NewStatic(nil), 200},
		{"unavailable", nil, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, owner, _ := pricingFixture(t, tc.catalog)
			result := request(handler, "GET", "/api/pricing", "", nil, owner)
			if result.Code != tc.status {
				t.Fatal(result.Code, result.Body.String())
			}
			if tc.status == 200 {
				var body struct {
					Prices []json.RawMessage `json:"prices"`
				}
				if json.Unmarshal(result.Body.Bytes(), &body) != nil || body.Prices == nil || len(body.Prices) != 0 {
					t.Fatal("empty price catalog", result.Body.String())
				}
			}
		})
	}
}
