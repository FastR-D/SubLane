package server

import (
	"bytes"
	"context"
	"encoding/json"
	"filippo.io/age"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/vault"
)

func TestBackupManagementExportsVerifiesAndPreparesIsolatedRestore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	connection, err := testdb.Open(ctx, filepath.Join(directory, "sublane.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-pass"); err != nil {
		t.Fatal(err)
	}
	member, err := identity.Login(ctx, "synthetic-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "credentials.key")
	if _, err := vault.Open(keyFile, true); err != nil {
		t.Fatal(err)
	}
	h := New(Options{DataDir: directory, CredentialsKeyFile: keyFile, Version: "synthetic-version", Auth: identity, Audit: audit.New(connection)})
	adminCookie := &http.Cookie{Name: sessionCookie, Value: admin.Token}
	memberCookie := &http.Cookie{Name: sessionCookie, Value: member.Token}
	path := "/api/settings/backup"
	if got := request(h, "GET", path, "", nil, nil); got.Code != 401 {
		t.Fatal(got.Code)
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		if got := request(h, method, path, "http://example.test", map[string]any{}, memberCookie); got.Code != 403 {
			t.Fatal("member boundary", method, got.Code)
		}
	}
	if got := request(h, "POST", path+"/export", "https://foreign.example.test", map[string]any{}, adminCookie); got.Code != 403 {
		t.Fatal("export origin", got.Code)
	}
	if rejected := request(h, "POST", path+"/export", "http://example.test", map[string]any{}, adminCookie); rejected.Code != 400 {
		t.Fatal("export omitted encryption password", rejected.Code)
	}
	exported := request(h, "POST", path+"/export", "http://example.test", map[string]any{"password": "synthetic backup passphrase"}, adminCookie)
	if exported.Code != 200 || exported.Header().Get("Content-Type") != "application/octet-stream" || exported.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("export", exported.Code, exported.Header())
	}
	uploadPassword := func(route string, body []byte, cookie *http.Cookie, password string) *httptest.ResponseRecorder {
		var payload bytes.Buffer
		writer := multipart.NewWriter(&payload)
		if err := writer.WriteField("password", password); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("file", "synthetic.age")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "http://example.test"+route, &payload)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Origin", "http://example.test")
		req.AddCookie(cookie)
		result := httptest.NewRecorder()
		h.ServeHTTP(result, req)
		return result
	}
	upload := func(route string, body []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
		return uploadPassword(route, body, cookie, "synthetic backup passphrase")
	}
	settings := request(h, "GET", path, "", nil, adminCookie)
	if !strings.Contains(settings.Body.String(), `"key_separated":true`) {
		t.Fatal("external key state missing", settings.Body.String())
	}
	wrong := uploadPassword(path+"/verify", exported.Body.Bytes(), adminCookie, "incorrect synthetic passphrase")
	if wrong.Code != 400 || !strings.Contains(wrong.Body.String(), "backup_decryption_failed") {
		t.Fatal("wrong password accepted", wrong.Code, wrong.Body.String())
	}
	identityKey, err := age.NewScryptIdentity("synthetic backup passphrase")
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := age.Decrypt(bytes.NewReader(exported.Body.Bytes()), identityKey)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := io.ReadAll(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	legacyRequest := httptest.NewRequest("POST", "http://example.test"+path+"/verify", bytes.NewReader(legacy))
	legacyRequest.Header.Set("Content-Type", "application/gzip")
	legacyRequest.Header.Set("Origin", "http://example.test")
	legacyRequest.AddCookie(adminCookie)
	legacyResponse := httptest.NewRecorder()
	h.ServeHTTP(legacyResponse, legacyRequest)
	if legacyResponse.Code != 200 || !strings.Contains(legacyResponse.Body.String(), `"encrypted":false`) {
		t.Fatal("legacy raw upload failed", legacyResponse.Code, legacyResponse.Body.String())
	}
	var duplicate bytes.Buffer
	duplicateWriter := multipart.NewWriter(&duplicate)
	if err := duplicateWriter.WriteField("password", "synthetic backup passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := duplicateWriter.WriteField("password", "another synthetic value"); err != nil {
		t.Fatal(err)
	}
	if err := duplicateWriter.Close(); err != nil {
		t.Fatal(err)
	}
	duplicateRequest := httptest.NewRequest("POST", "http://example.test"+path+"/verify", &duplicate)
	duplicateRequest.Header.Set("Content-Type", duplicateWriter.FormDataContentType())
	duplicateRequest.Header.Set("Origin", "http://example.test")
	duplicateRequest.AddCookie(adminCookie)
	duplicateResponse := httptest.NewRecorder()
	h.ServeHTTP(duplicateResponse, duplicateRequest)
	if duplicateResponse.Code != 400 {
		t.Fatal("duplicate password field accepted", duplicateResponse.Code)
	}
	if got := upload(path+"/verify", exported.Body.Bytes(), memberCookie); got.Code != 403 {
		t.Fatal("member verify", got.Code)
	}
	oversized := httptest.NewRequest("POST", "http://example.test"+path+"/verify", bytes.NewReader(nil))
	oversized.AddCookie(adminCookie)
	oversized.Header.Set("Origin", "http://example.test")
	oversized.Header.Set("Content-Type", "application/gzip")
	oversized.ContentLength = maxWebBackupBytes + 1
	rejected := httptest.NewRecorder()
	h.ServeHTTP(rejected, oversized)
	if rejected.Code != 413 {
		t.Fatal("upload size limit", rejected.Code)
	}
	verified := upload(path+"/verify", exported.Body.Bytes(), adminCookie)
	if verified.Code != 200 {
		t.Fatal("verify", verified.Code, verified.Body.String())
	}
	if got := upload(path+"/verify", []byte("synthetic-invalid-archive"), adminCookie); got.Code != 400 {
		t.Fatal("invalid backup", got.Code)
	}
	prepared := upload(path+"/restore", exported.Body.Bytes(), adminCookie)
	if prepared.Code != 201 {
		t.Fatal("restore", prepared.Code, prepared.Body.String())
	}
	var value struct {
		Directory string `json:"restore_directory"`
	}
	if err := json.Unmarshal(prepared.Body.Bytes(), &value); err != nil || value.Directory != filepath.Join(directory, "restore-ready") {
		t.Fatal("restore directory", err)
	}
	if _, err := os.Stat(filepath.Join(value.Directory, "sublane.db")); err != nil {
		t.Fatal(err)
	}
	if got := upload(path+"/restore", exported.Body.Bytes(), adminCookie); got.Code != 409 {
		t.Fatal("overwrote prepared restore", got.Code)
	}
	if state, err := identity.State(ctx, admin.Token); err != nil || state.User == nil {
		t.Fatal("live instance changed", err)
	}
	var successes int
	if err := connection.QueryRow("SELECT count(*) FROM audit_events WHERE resource='backup' AND outcome='success'").Scan(&successes); err != nil || successes != 2 {
		t.Fatal("backup audit", successes, err)
	}
}

type backupDeadlines struct {
	*httptest.ResponseRecorder
	read, write time.Time
}

func (w *backupDeadlines) SetReadDeadline(value time.Time) error  { w.read = value; return nil }
func (w *backupDeadlines) SetWriteDeadline(value time.Time) error { w.write = value; return nil }
func TestBackupOperationReleasesCapacityAndResetsDeadlines(t *testing.T) {
	h := &backupHTTP{slots: make(chan struct{}, 1)}
	output := &backupDeadlines{ResponseRecorder: httptest.NewRecorder()}
	h.operation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if output.read.IsZero() || output.write.IsZero() {
			t.Fatal("backup IO has no deadline")
		}
		w.WriteHeader(204)
	})).ServeHTTP(output, httptest.NewRequest("POST", "/", nil))
	if len(h.slots) != 0 || !output.read.IsZero() || !output.write.IsZero() {
		t.Fatal("backup affected the next keep-alive request")
	}
	h.slots <- struct{}{}
	blocked := httptest.NewRecorder()
	h.operation(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("parallel backup admitted") })).ServeHTTP(blocked, httptest.NewRequest("POST", "/", nil))
	if blocked.Code != 429 {
		t.Fatal(blocked.Code)
	}
}

func TestBackupOperationCancellationInterruptsStalledUpload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &backupHTTP{slots: make(chan struct{}, 1)}
	started, finished := make(chan struct{}), make(chan struct{})
	server := httptest.NewUnstartedServer(h.operation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, _ = io.Copy(io.Discard, r.Body)
		close(finished)
	})))
	server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	server.Start()
	defer server.Close()
	connection, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "POST / HTTP/1.1\r\nHost: example.test\r\nContent-Length: 10\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload did not begin")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		connection.Close()
		t.Fatal("cancellation left the upload blocked")
	}
}
