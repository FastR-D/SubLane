package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestPersonalRequestsEnforceOwnershipBeforePagination(t *testing.T) {
	ctx := context.Background()
	connection, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "owner-test", "owner pass 42", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "member-test", "member pass 42")
	if err != nil {
		t.Fatal(err)
	}
	other, err := identity.CreateMember(ctx, "other-test", "other pass 42")
	if err != nil {
		t.Fatal(err)
	}
	configureTestPool(t, connection)
	cookie := func(username, password string) *http.Cookie {
		login, err := identity.Login(ctx, username, password)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: sessionCookie, Value: login.Token}
	}
	ownerCookie := cookie("owner-test", "owner pass 42")
	memberCookie := cookie("member-test", "member pass 42")
	service := gateway.New(ctx, connection, nil, nil)
	t.Cleanup(service.Close)
	h := New(Options{Auth: identity, Gateway: service})
	q := db.New(connection)
	for i := 0; i < 120; i++ {
		userID, outcome := member.ID, "success"
		if i%2 == 0 {
			userID, outcome = other.ID, "error"
		}
		if err := q.RecordRequest(ctx, db.RecordRequestParams{UserID: userID, KeyID: 100, GroupID: 1, AccountID: "private-subscription", Provider: "codex", Model: "codex/synthetic-model", Transport: "http", Operation: "responses", StartedAt: time.Now().Unix(), Outcome: outcome}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string, session *http.Cookie) gateway.RequestPage {
		t.Helper()
		result := request(h, "GET", path, "", nil, session)
		var page gateway.RequestPage
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &page) != nil {
			t.Fatalf("history status=%d body=%s", result.Code, result.Body.String())
		}
		return page
	}
	for _, test := range []struct {
		path   string
		cookie *http.Cookie
		count  int
	}{{"/api/me/requests/filters", memberCookie, 1}, {"/api/requests/filters", ownerCookie, 2}} {
		response := request(h, "GET", test.path, "", nil, test.cookie)
		var value struct {
			Callers []struct {
				UserID int64 `json:"user_id"`
				KeyID  int64 `json:"key_id"`
			} `json:"callers"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &value) != nil || len(value.Callers) != test.count {
			t.Fatal("filter choices unavailable", response.Code, response.Body.String())
		}
		if test.count == 1 && value.Callers[0].UserID != member.ID {
			t.Fatal("personal choices leaked other members")
		}
	}
	if got := request(h, "GET", "/api/requests/filters", "", nil, memberCookie).Code; got != 403 {
		t.Fatal("member read operator filter metadata", got)
	}
	first := read(fmt.Sprintf("/api/me/requests?user_id=%d&scope=all&account_id=private-subscription", other.ID), memberCookie)
	if len(first.Requests) != 50 || first.NextCursor == 0 {
		t.Fatal("ownership was not applied before pagination", len(first.Requests))
	}
	second := read(fmt.Sprintf("/api/me/requests?cursor=%d", first.NextCursor), memberCookie)
	if len(second.Requests) != 10 || second.NextCursor != 0 {
		t.Fatal("incorrect personal second page")
	}
	for _, record := range append(first.Requests, second.Requests...) {
		if record.UserID != member.ID || record.AccountID != "" || record.AccountName != "" {
			t.Fatal("personal history exposed another user or subscription", record.ID)
		}
	}
	if page := read("/api/me/requests?outcome=error", memberCookie); len(page.Requests) != 0 {
		t.Fatal("result filter escaped ownership")
	}
	if page := read("/api/me/requests", ownerCookie); len(page.Requests) != 0 {
		t.Fatal("administrator personal view included other users")
	}
	if page := read("/api/requests", ownerCookie); len(page.Requests) != 50 || page.Requests[0].AccountID != "private-subscription" {
		t.Fatal("administrator history lost operator details")
	}
	for _, tc := range []struct {
		path                    string
		cookie                  *http.Cookie
		total, page, size, rows int64
	}{
		{"/api/me/requests", memberCookie, 60, 1, 50, 50},
		{"/api/me/requests?page=3&limit=25", memberCookie, 60, 3, 25, 10},
		{"/api/me/requests?page=999&limit=25", memberCookie, 60, 3, 25, 10},
		{fmt.Sprintf("/api/me/requests?page=2&limit=25&user_id=%d&account_id=private-subscription", other.ID), memberCookie, 60, 2, 25, 25},
		{"/api/me/requests?outcome=error&page=4&limit=10", memberCookie, 0, 1, 10, 0},
		{"/api/me/requests", ownerCookie, 0, 1, 50, 0},
		{"/api/requests?page=2&limit=100", ownerCookie, 120, 2, 100, 20},
		{"/api/requests?limit=100", ownerCookie, 120, 1, 100, 100},
		{"/api/requests?outcome=error&page=3&limit=25", ownerCookie, 60, 3, 25, 10},
	} {
		t.Run(tc.path, func(t *testing.T) {
			result := request(h, "GET", tc.path, "", nil, tc.cookie)
			var page struct {
				Requests []gateway.RequestRecord `json:"requests"`
				Total    int64                   `json:"total"`
				Page     int64                   `json:"page"`
				PageSize int64                   `json:"page_size"`
			}
			if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &page) != nil {
				t.Fatalf("history status=%d body=%s", result.Code, result.Body.String())
			}
			if page.Total != tc.total || page.Page != tc.page || page.PageSize != tc.size || int64(len(page.Requests)) != tc.rows {
				t.Fatalf("pagination total=%d page=%d size=%d rows=%d, want %d/%d/%d/%d", page.Total, page.Page, page.PageSize, len(page.Requests), tc.total, tc.page, tc.size, tc.rows)
			}
			if tc.cookie == memberCookie {
				for _, record := range page.Requests {
					if record.UserID != member.ID || record.AccountID != "" || record.AccountName != "" {
						t.Fatal("numbered pagination escaped personal ownership", record.ID)
					}
				}
			}
		})
	}
	for _, query := range []string{"page=-1", "page=bad", "limit=-1", "limit=101", "limit=bad", "cursor=1&page=2"} {
		if got := request(h, "GET", "/api/me/requests?"+query, "", nil, memberCookie).Code; got != 400 {
			t.Error("invalid pagination accepted", query, got)
		}
	}
	for _, path := range []string{"/api/me/requests?cursor=-1", "/api/me/requests?cursor=bad", "/api/me/requests?outcome=invalid"} {
		if got := request(h, "GET", path, "", nil, memberCookie).Code; got != 400 {
			t.Fatal("invalid filter accepted", path, got)
		}
	}
	if got := request(h, "GET", "/api/me/requests", "", nil, nil).Code; got != 401 {
		t.Fatal("anonymous history access", got)
	}
	if got := request(h, "GET", "/api/requests", "", nil, memberCookie).Code; got != 403 {
		t.Fatal("member accessed all history", got)
	}
	if _, err := service.UserRequests(ctx, 0, gateway.RequestFilter{}); err == nil {
		t.Fatal("missing personal identity became an administrator query")
	}
	if _, err := identity.SetMemberEnabled(ctx, member.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := request(h, "GET", "/api/me/requests", "", nil, memberCookie).Code; got != 401 {
		t.Fatal("disabled member accessed history", got)
	}
}

func TestRuntimeManagementRemainsAdministratorOnly(t *testing.T) {
	fixture := newForwardFixture(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected provider request") })
	h := fixture.server.Config.Handler
	cookie := func(username, password string) *http.Cookie {
		value, err := fixture.identity.Login(context.Background(), username, password)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: sessionCookie, Value: value.Token}
	}
	owner := cookie("owner-test", "owner pass 42")
	member := cookie("member-test", "member pass 42")
	accounts, err := fixture.accounts.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := accounts[0].ID
	for _, path := range []string{"/api/accounts/runtime", "/api/requests"} {
		if got := request(h, "GET", path, "", nil, member).Code; got != 403 {
			t.Fatal("member accessed operator data", path, got)
		}
		if got := request(h, "GET", path, "", nil, owner).Code; got != 200 {
			t.Fatal("operator endpoint unavailable", path, got)
		}
	}
	if got := request(h, "PATCH", "/api/accounts/"+id+"/limits", "http://example.test", map[string]int{"max_concurrency": 1}, member).Code; got != 403 {
		t.Fatal("member changed pool policy", got)
	}
	if got := request(h, "PATCH", "/api/accounts/"+id+"/limits", "http://foreign.example.test", map[string]int{"max_concurrency": 1}, owner).Code; got != 403 {
		t.Fatal("cross-origin policy update", got)
	}
	for _, limit := range []int64{1, 8, 30, 64, 1000} {
		result := request(h, "PATCH", "/api/accounts/"+id+"/limits", "http://example.test", map[string]int64{"max_concurrency": limit}, owner)
		var updated struct {
			Max int64 `json:"max_concurrency"`
		}
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &updated) != nil || updated.Max != limit {
			t.Fatalf("policy %d not saved: %d %s", limit, result.Code, result.Body.String())
		}
		for _, path := range []string{"/api/accounts", "/api/accounts/runtime"} {
			listed := request(h, "GET", path, "", nil, owner)
			var page struct {
				Accounts []struct {
					Max int64 `json:"max_concurrency"`
				} `json:"accounts"`
			}
			if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &page) != nil || len(page.Accounts) != 1 || page.Accounts[0].Max != limit {
				t.Fatalf("%s did not return saved limit %d: %d %s", path, limit, listed.Code, listed.Body.String())
			}
		}
	}
	for _, limit := range []int{0, 1001} {
		if got := request(h, "PATCH", "/api/accounts/"+id+"/limits", "http://example.test", map[string]int{"max_concurrency": limit}, owner).Code; got != 400 {
			t.Fatal("invalid concurrency accepted", limit, got)
		}
	}
	if got := request(h, "POST", "/api/accounts/"+id+"/resume", "http://example.test", map[string]any{}, owner).Code; got != 204 {
		t.Fatal("resume failed", got)
	}
	if got := request(h, "GET", "/api/requests?outcome=arbitrary", "", nil, owner).Code; got != 400 {
		t.Fatal("invalid filter accepted", got)
	}
}
