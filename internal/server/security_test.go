package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/vault"
	"github.com/pquerna/otp/totp"
)

func TestSecurityHTTPFlow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	conn, err := testdb.Open(ctx, filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cipher, err := vault.Open(filepath.Join(dir, "credentials.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := auth.NewWithVault(conn, cipher)
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Tenants: tenants.New(conn), PublicURL: "https://example.test"})
	setup := request(h, "POST", "/api/auth/setup", "https://example.test", map[string]string{"username": "security-test", "password": "synthetic-password", "workspace_name": "Synthetic"}, nil)
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	for _, path := range []string{"/api/me/security", "/api/me/totp", "/api/me/passkeys"} {
		if got := request(h, "POST", path, "https://example.test", map[string]string{}, nil).Code; got != 401 {
			t.Fatalf("unprotected %s: %d", path, got)
		}
	}
	if got := request(h, "POST", "/api/me/totp", "https://evil.test", map[string]string{}, cookie).Code; got != 403 {
		t.Fatal("cross-origin setup", got)
	}
	begin := request(h, "POST", "/api/me/totp", "https://example.test", map[string]string{"current_password": "synthetic-password"}, cookie)
	if begin.Code != 200 {
		t.Fatalf("begin: %d %s", begin.Code, begin.Body.String())
	}
	var pending struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(begin.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	challenges := begin.Result().Cookies()
	if len(challenges) != 1 || !challenges[0].HttpOnly || !challenges[0].Secure || challenges[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe challenge cookie")
	}
	code, _ := totp.GenerateCode(pending.Secret, time.Now())
	input := map[string]string{"code": code}
	r := requestWithChallenge(h, "/api/me/totp/confirm", input, cookie, challenges[0])
	if r.Code != 200 {
		t.Fatalf("confirm: %d %s", r.Code, r.Body.String())
	}
	login := request(h, "POST", "/api/auth/login", "https://example.test", map[string]string{"username": "security-test", "password": "synthetic-password"}, nil)
	if login.Code != 401 || !strings.Contains(login.Body.String(), "two_factor_required") || len(login.Result().Cookies()) != 0 {
		t.Fatalf("password bypass: %d %s", login.Code, login.Body.String())
	}
	var codes struct {
		Codes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &codes); err != nil {
		t.Fatal(err)
	}
	login = request(h, "POST", "/api/auth/login", "https://example.test", map[string]string{"username": "security-test", "password": "synthetic-password", "code": codes.Codes[0]}, nil)
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatalf("2FA login: %d %s", login.Code, login.Body.String())
	}
	passkey := request(h, "POST", "/api/auth/passkey/begin", "https://example.test", map[string]string{}, nil)
	if passkey.Code != 200 || !strings.Contains(passkey.Body.String(), `"userVerification":"required"`) {
		t.Fatalf("passkey options: %d %s", passkey.Code, passkey.Body.String())
	}
}

func requestWithChallenge(h http.Handler, path string, body any, session, challenge *http.Cookie) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "https://example.test"+path, bytes.NewReader(data))
	r.Header.Set("Origin", "https://example.test")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(session)
	r.AddCookie(challenge)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFactorRateLimitHTTPResponse(t *testing.T) {
	w := httptest.NewRecorder()
	authError(w, &auth.FactorRateLimitError{RetryAfter: 42})
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "42" || !strings.Contains(w.Body.String(), `"rate_limited"`) {
		t.Fatalf("lost factor retry interval: %d %s", w.Code, w.Body.String())
	}
}
