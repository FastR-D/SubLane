package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/testdb"
)

func authFixture(t *testing.T, publicURL string) http.Handler {
	t.Helper()
	dir := t.TempDir()
	db, err := testdb.Open(context.Background(), filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{Auth: a, Tenants: tenants.New(db), PublicURL: publicURL, Ping: db.PingContext, StartedAt: time.Now(), Version: "test"})
}

func request(h http.Handler, method, path, origin string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	r := httptest.NewRequest(method, "http://example.test"+path, reader)
	r.RemoteAddr = "192.0.2.1:1234"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthenticationHTTPFlow(t *testing.T) {
	h := authFixture(t, "")
	state := request(h, "GET", "/api/auth/state", "", nil, nil)
	if state.Code != 200 || !strings.Contains(state.Body.String(), `"initialized":false`) {
		t.Fatal(state.Body.String())
	}
	if got := request(h, "GET", "/api/system", "", nil, nil).Code; got != 401 {
		t.Fatalf("unprotected system: %d", got)
	}
	body := map[string]string{"username": "admin-test", "password": "fake password 42", "workspace_name": "Synthetic workspace"}
	setup := request(h, "POST", "/api/auth/setup", "http://example.test", body, nil)
	if setup.Code != 201 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	cookies := setup.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 43200 {
		t.Fatal("bad cookie flags")
	}
	if strings.Contains(setup.Body.String(), cookie.Value) {
		t.Fatal("secret exposed in JSON")
	}
	if got := request(h, "POST", "/api/auth/setup", "http://example.test", body, nil).Code; got != 409 {
		t.Fatalf("setup remains open: %d", got)
	}
	if got := request(h, "GET", "/api/system", "", nil, cookie).Code; got != 200 {
		t.Fatalf("valid session denied: %d", got)
	}
	logout := request(h, "POST", "/api/auth/logout", "http://example.test", map[string]string{}, cookie)
	if logout.Code != 200 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout failed: %d", logout.Code)
	}
	if got := request(h, "GET", "/api/system", "", nil, cookie).Code; got != 401 {
		t.Fatalf("revoked token accepted: %d", got)
	}
	login := request(h, "POST", "/api/auth/login", "http://example.test", map[string]string{"username": "admin-test", "password": "fake password 42"}, nil)
	if login.Code != 200 || login.Result().Cookies()[0].Value == cookie.Value {
		t.Fatalf("login failed: %d", login.Code)
	}
	if got := request(h, "GET", "/api/future-management", "", nil, nil).Code; got != 401 {
		t.Fatalf("management subtree is not default-protected: %d", got)
	}
	if got := request(h, "GET", "/healthz", "", nil, nil).Code; got != 200 {
		t.Fatal("liveness requires auth")
	}
}

func TestInvitationHTTPFlow(t *testing.T) {
	h := authFixture(t, "")
	setup := request(h, "POST", "/api/auth/setup", "http://example.test", map[string]string{
		"username": "owner-test", "password": "synthetic-pass", "workspace_name": "Synthetic workspace",
	}, nil)
	if setup.Code != 201 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	owner := setup.Result().Cookies()[0]
	if got := request(h, "POST", "/api/members/invitations", "http://example.test", map[string]string{}, nil).Code; got != 401 {
		t.Fatalf("anonymous invitation creation: %d", got)
	}
	created := request(h, "POST", "/api/members/invitations", "http://example.test", map[string]string{}, owner)
	if created.Code != 201 {
		t.Fatalf("create invitation: %d %s", created.Code, created.Body.String())
	}
	var invitation struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &invitation); err != nil || invitation.Token == "" {
		t.Fatalf("invalid invitation response: %v %s", err, created.Body.String())
	}
	input := map[string]string{"token": invitation.Token, "username": "invited-test", "password": "synthetic-pass"}
	if got := request(h, "POST", "/api/auth/register", "https://other.example.test", input, nil).Code; got != 403 {
		t.Fatalf("cross-origin registration: %d", got)
	}
	registered := request(h, "POST", "/api/auth/register", "http://example.test", input, nil)
	if registered.Code != 201 || !strings.Contains(registered.Body.String(), `"role":"member"`) || len(registered.Result().Cookies()) != 1 {
		t.Fatalf("registration: %d %s", registered.Code, registered.Body.String())
	}
	if got := request(h, "POST", "/api/auth/register", "http://example.test", map[string]string{
		"token": invitation.Token, "username": "second-test", "password": "synthetic-pass",
	}, nil).Code; got != 410 {
		t.Fatalf("reused invitation: %d", got)
	}
	member := registered.Result().Cookies()[0]
	if got := request(h, "POST", "/api/members/invitations", "http://example.test", map[string]string{}, member).Code; got != 403 {
		t.Fatalf("member invitation creation: %d", got)
	}
}

func TestSetupRequiresNamedWorkspace(t *testing.T) {
	h := authFixture(t, "")
	credentials := map[string]string{"username": "synthetic-admin", "password": "synthetic-password"}
	if result := request(h, http.MethodPost, "/api/auth/setup", "http://example.test", credentials, nil); result.Code != http.StatusBadRequest {
		t.Fatalf("setup without workspace name: %d %s", result.Code, result.Body.String())
	}
	credentials["workspace_name"] = "  "
	if result := request(h, http.MethodPost, "/api/auth/setup", "http://example.test", credentials, nil); result.Code != http.StatusBadRequest {
		t.Fatalf("setup with blank workspace name: %d %s", result.Code, result.Body.String())
	}
	if state := request(h, http.MethodGet, "/api/auth/state", "", nil, nil); !strings.Contains(state.Body.String(), `"initialized":false`) {
		t.Fatalf("rejected workspace initialized the instance: %s", state.Body.String())
	}
	credentials["workspace_name"] = "  Synthetic studio  "
	created := request(h, http.MethodPost, "/api/auth/setup", "http://example.test", credentials, nil)
	if created.Code != http.StatusCreated || len(created.Result().Cookies()) != 1 {
		t.Fatalf("named workspace setup: %d %s", created.Code, created.Body.String())
	}
	listed := request(h, http.MethodGet, "/api/tenants", "", nil, created.Result().Cookies()[0])
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"name":"Synthetic studio"`) {
		t.Fatalf("first workspace name was not persisted: %d %s", listed.Code, listed.Body.String())
	}
}

func TestAuthOriginAndBodyProtection(t *testing.T) {
	h := authFixture(t, "")
	body := map[string]string{"username": "admin-test", "password": "fake password 42", "workspace_name": "Synthetic workspace"}
	for _, origin := range []string{"", "null", "https://evil.example.test", "http://example.test.evil.test"} {
		if got := request(h, "POST", "/api/auth/setup", origin, body, nil).Code; got != 403 {
			t.Fatalf("origin %q accepted: %d", origin, got)
		}
	}
	for _, tc := range []struct {
		content, body string
		status        int
	}{
		{"text/plain", `{}`, 415},
		{"application/json", `{"unknown":true}`, 400},
		{"application/json", `{} {}`, 400},
		{"application/json", `{"username":"` + strings.Repeat("x", 5000) + `"}`, 413},
	} {
		r := httptest.NewRequest("POST", "http://example.test/api/auth/login", strings.NewReader(tc.body))
		r.Header.Set("Origin", "http://example.test")
		r.Header.Set("Content-Type", tc.content)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("body guard: got %d want %d", w.Code, tc.status)
		}
	}
	r := httptest.NewRequest("POST", "http://example.test/api/auth/setup", strings.NewReader(`{}`))
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site metadata accepted")
	}
}

func TestConfiguredOriginUsesSecureCookie(t *testing.T) {
	h := authFixture(t, "https://gateway.example.test")
	body := map[string]string{"username": "admin-test", "password": "fake password 42", "workspace_name": "Synthetic workspace"}
	if got := request(h, "POST", "/api/auth/setup", "http://example.test", body, nil).Code; got != 403 {
		t.Fatal("proxy backend origin trusted")
	}
	w := request(h, "POST", "/api/auth/setup", "https://gateway.example.test", body, nil)
	if w.Code != 201 || !w.Result().Cookies()[0].Secure {
		t.Fatal("external HTTPS did not produce a Secure cookie")
	}
}

func TestLoginThrottleIgnoresForwardedPeerHeaders(t *testing.T) {
	h := authFixture(t, "")
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "http://example.test/api/auth/login", strings.NewReader(`{"username":"unknown-test","password":"fake password 42"}`))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Origin", "http://example.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", strings.Repeat("1", i+1)+".example.test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i < 5 && w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
		if i == 5 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("throttle bypassed with forwarded headers")
		}
	}
}

func TestTrustedProxyUsesRightmostUntrustedAddress(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("198.51.100.0/24")}
	r := httptest.NewRequest(http.MethodPost, "http://example.test/api/auth/login", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 192.0.2.4, 198.51.100.8")
	if got := peerAddress(r, trusted); got != "192.0.2.4" {
		t.Fatalf("trusted chain selected %q", got)
	}
	r.RemoteAddr = "192.0.2.10:1234"
	if got := peerAddress(r, trusted); got != "192.0.2.10" {
		t.Fatalf("untrusted direct peer selected %q", got)
	}
}

func TestIndependentPeersDoNotShareGlobalLoginBucket(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < 60; i++ {
		if retry := l.allow("192.0.2." + strconv.Itoa(i)); retry != 0 {
			t.Fatalf("independent peer %d was blocked: %d", i, retry)
		}
	}
	if retry := l.allow("198.51.100.1"); retry != 0 {
		t.Fatalf("one peer exhausted a site-wide bucket: %d", retry)
	}
}

func TestInvitationRegistrationDoesNotConsumeLoginAttempts(t *testing.T) {
	h := authFixture(t, "")
	for i := 0; i < 5; i++ {
		result := request(h, http.MethodPost, "/api/auth/register", "http://example.test", map[string]string{
			"token": "synthetic-invalid-token", "username": "synthetic-user", "password": "synthetic-password",
		}, nil)
		if result.Code == http.StatusTooManyRequests {
			t.Fatalf("registration attempt %d was throttled", i)
		}
	}
	result := request(h, http.MethodPost, "/api/auth/login", "http://example.test", map[string]string{
		"username": "synthetic-user", "password": "synthetic-password",
	}, nil)
	if result.Code == http.StatusTooManyRequests {
		t.Fatal("registration exhausted the login bucket")
	}
}

func TestLoginAttemptsAreSharedAcrossWorkspaceHandlers(t *testing.T) {
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
	shared := NewLoginLimiter()
	first := New(Options{Auth: identity, LoginLimiter: shared})
	second := New(Options{Auth: identity, TenantID: 2, LoginLimiter: shared})
	body := map[string]string{"username": "synthetic-user", "password": "synthetic-password"}
	for i := 0; i < 5; i++ {
		if got := request(first, http.MethodPost, "/api/auth/login", "http://example.test", body, nil).Code; got == http.StatusTooManyRequests {
			t.Fatalf("early login throttle: %d", i)
		}
	}
	if got := request(second, http.MethodPost, "/api/auth/login", "http://example.test", body, nil).Code; got != http.StatusTooManyRequests {
		t.Fatalf("workspace switch bypassed login limit: %d", got)
	}
}

func TestLimiterExpiresAndBoundsPeerState(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newLoginLimiter()
	l.now = func() time.Time { return now }
	for range 5 {
		if l.allow("192.0.2.1") != 0 {
			t.Fatal("early throttle")
		}
	}
	if l.allow("192.0.2.1") <= 0 {
		t.Fatal("missing peer limit")
	}
	now = now.Add(time.Minute)
	if l.allow("192.0.2.1") != 0 {
		t.Fatal("peer limit did not expire")
	}
	l.peers = make(map[string]attemptWindow)
	for i := 0; i < maxLoginPeers; i++ {
		lastSeen := now
		if i == 0 {
			lastSeen = now.Add(-time.Minute)
		}
		l.peers[string(rune(i))] = attemptWindow{reset: now.Add(time.Minute), lastSeen: lastSeen}
	}
	if l.allow("new-peer") != 0 || len(l.peers) != maxLoginPeers {
		t.Fatal("new peer was blocked or peer state grew without bound")
	}
	if _, exists := l.peers[string(rune(0))]; exists {
		t.Fatal("oldest peer was not evicted")
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (d *deadlineRecorder) SetReadDeadline(value time.Time) error {
	d.deadlines = append(d.deadlines, value)
	return nil
}

func TestAuthBodyReadsHaveADeadline(t *testing.T) {
	h := authFixture(t, "")
	r := httptest.NewRequest("POST", "http://example.test/api/auth/login", strings.NewReader(`{"username":"unknown-test","password":"fake password 42"}`))
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", "application/json")
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(w, r)
	if len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Fatal("authentication body reads need a bounded deadline that is cleared afterward")
	}
}
