package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestRouterGroupsProtectMethodErrorsAndFallbacks(t *testing.T) {
	ctx := context.Background()
	db, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "routing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	identity, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := identity.Setup(ctx, "owner-test", "owner pass 42", "Synthetic workspace")
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "member-test", "member pass 42")
	if err != nil {
		t.Fatal(err)
	}
	session, err := identity.Login(ctx, member.Username, "member pass 42")
	if err != nil {
		t.Fatal(err)
	}
	configureTestPool(t, db)
	keys := newTestKeyService(t, db)
	key, err := keys.CreateInGroup(ctx, member.ID, 1, "Synthetic client")
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Keys: keys, Ping: db.PingContext})
	for _, tc := range []struct {
		name, method, path, token, bearer, origin string
		status                                    int
		allow                                     string
	}{
		{name: "anonymous member method", method: "DELETE", path: "/api/members", status: 401},
		{name: "member management method", method: "DELETE", path: "/api/members", token: session.Token, status: 403},
		{name: "owner member method", method: "DELETE", path: "/api/members", token: owner.Token, status: 405, allow: "GET, POST"},
		{name: "owner status method", method: "GET", path: "/api/members/2", token: owner.Token, status: 405, allow: "PATCH, DELETE"},
		{name: "member status method", method: "GET", path: "/api/members/2", token: session.Token, status: 403},
		{name: "personal key method", method: "DELETE", path: "/api/keys", token: session.Token, status: 405, allow: "GET, POST"},
		{name: "personal revoke method", method: "GET", path: "/api/keys/1/revoke", token: session.Token, status: 405, allow: "POST"},
		{name: "personal mutation origin", method: "POST", path: "/api/keys", token: session.Token, origin: "https://other.example.test", status: 403},
		{name: "anonymous key fallback", method: "GET", path: "/api/keys/1/missing", status: 401},
		{name: "member key fallback", method: "GET", path: "/api/keys/1/missing", token: session.Token, status: 403},
		{name: "owner key fallback", method: "GET", path: "/api/keys/1/missing", token: owner.Token, status: 404},
		{name: "member auth fallback", method: "GET", path: "/api/auth/missing", token: session.Token, status: 403},
		{name: "anonymous login method", method: "PUT", path: "/api/auth/login", status: 405, allow: "POST"},
		{name: "explicit state methods", method: "HEAD", path: "/api/auth/state", status: 405, allow: "GET"},
		{name: "anonymous gateway method", method: "DELETE", path: "/v1/models", status: 401},
		{name: "gateway ignores session", method: "DELETE", path: "/v1/models", token: owner.Token, status: 401},
		{name: "gateway model method", method: "DELETE", path: "/v1/models", bearer: key.Secret, status: 405, allow: "GET"},
		{name: "gateway response method", method: "GET", path: "/v1/responses", bearer: key.Secret, status: 405, allow: "POST"},
		{name: "gateway unavailable", method: "POST", path: "/v1/chat/completions", bearer: key.Secret, status: 501},
		{name: "anonymous gateway fallback", method: "PUT", path: "/v1/missing", status: 401},
		{name: "gateway fallback", method: "PUT", path: "/v1/missing", bearer: key.Secret, status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://example.test"+tc.path, nil)
			origin := tc.origin
			if origin == "" {
				origin = "http://example.test"
			}
			r.Header.Set("Origin", origin)
			if tc.token != "" {
				r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tc.token})
			}
			if tc.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("status=%d allow=%s", w.Code, w.Header().Get("Allow"))
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("API error response must be JSON and non-cacheable")
			}
		})
	}
}
