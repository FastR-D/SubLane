package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

func newRefreshRegistry(t *testing.T, transport subscriptionTransport) *tenantRegistry {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	connection, err := storage.Open(ctx, filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-owner", "synthetic-password", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(t.TempDir(), "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	provider := upstream.NewWithTransport(transport)
	t.Cleanup(provider.Close)
	r := &tenantRegistry{ctx: ctx, db: connection, vault: cipher, auth: identity, provider: provider, refreshLead: accounts.CodexRefreshLead}
	t.Cleanup(r.Close)
	return r
}

func syntheticRotationResponse() *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic-new","refresh_token":"synthetic-rotated","expires_in":3600}`))}
}

func TestScheduledRefreshRunsWithoutTrafficAndSkipsIneligibleAccounts(t *testing.T) {
	var calls atomic.Int32
	r := newRefreshRegistry(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "auth.openai.com" || r.URL.Path != "/oauth/token" {
			t.Errorf("maintenance sent unexpected provider request")
		}
		calls.Add(1)
		return syntheticRotationResponse(), nil
	})
	ctx := r.ctx
	runtime := r.acquire(1)
	s := runtime.accounts
	var disabledID string
	for _, name := range []string{"due", "fresh", "disabled", "reauthorize"} {
		validity := time.Hour
		if name == "fresh" {
			validity = 10 * 24 * time.Hour
		}
		a, err := s.Authorize(ctx, name, accounts.Credential{AccountID: name, AccessToken: "synthetic-old", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(validity).Unix()}, "")
		if err != nil {
			t.Fatal(err)
		}
		if name == "disabled" {
			disabledID = a.ID
			if _, err := s.SetEnabled(ctx, a.ID, false); err != nil {
				t.Fatal(err)
			}
		}
		if name == "reauthorize" {
			if _, err := r.db.Exec("UPDATE accounts SET status='reauth_required' WHERE id=?", a.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.release(runtime)
	cursor, err := r.refreshAccounts(ctx, "")
	if err != nil || cursor != "" || calls.Load() != 2 {
		t.Fatalf("scheduled refresh: cursor=%q calls=%d err=%v", cursor, calls.Load(), err)
	}
	disabled, err := s.Get(ctx, disabledID)
	if err != nil || disabled.Enabled || disabled.Status != "ready" || disabled.ExpiresAt <= disabled.UpdatedAt {
		t.Fatalf("scheduled rotation changed disabled account availability: %+v err=%v", disabled, err)
	}
	if _, err := r.refreshAccounts(ctx, ""); err != nil || calls.Load() != 2 {
		t.Fatalf("maintenance repeated a successful rotation: %v", err)
	}
	// Recreating the workspace runtime must retain the rotation's durable refresh schedule.
	r.now = func() time.Time { return time.Now().Add(runtimeIdleTimeout + time.Minute) }
	r.reapIdle()
	if _, err := r.refreshAccounts(ctx, ""); err != nil || calls.Load() != 2 {
		t.Fatalf("idle runtime recovery lost rotation: %v", err)
	}
}

func authorizeDueAccount(t *testing.T, r *tenantRegistry, runtime *tenantRuntime) accounts.Account {
	t.Helper()
	a, err := runtime.accounts.Authorize(r.ctx, "Synthetic due account", accounts.Credential{AccountID: "synthetic-account", AccessToken: "synthetic-old", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestScheduledAndForegroundRefreshShareRotation(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	r := newRefreshRegistry(t, func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-resume
		return syntheticRotationResponse(), nil
	})
	t.Cleanup(func() { once.Do(func() { close(resume) }) })
	runtime := r.acquire(1)
	a := authorizeDueAccount(t, r, runtime)
	r.release(runtime)
	scheduled := make(chan error, 1)
	go func() { _, err := r.refreshAccounts(r.ctx, ""); scheduled <- err }()
	<-entered
	foreground := make(chan error, 1)
	go func() { _, err := runtime.accounts.Prepare(r.ctx, a.ID, r.provider.Refresh); foreground <- err }()
	once.Do(func() { close(resume) })
	if err := <-scheduled; err != nil {
		t.Fatal(err)
	}
	if err := <-foreground; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("foreground and maintenance rotated %d times", calls.Load())
	}
}

func TestRetiringRuntimeWaitsForRotationBeforeReplacement(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r := newRefreshRegistry(t, func(*http.Request) (*http.Response, error) {
		close(entered)
		<-resume
		return syntheticRotationResponse(), nil
	})
	t.Cleanup(func() { once.Do(func() { close(resume) }) })
	runtime := r.acquire(1)
	a := authorizeDueAccount(t, r, runtime)
	r.release(runtime)
	rotation := make(chan error, 1)
	go func() { _, err := runtime.accounts.Prepare(r.ctx, a.ID, r.provider.Refresh); rotation <- err }()
	<-entered
	r.now = func() time.Time { return time.Now().Add(runtimeIdleTimeout + time.Minute) }
	reaped := make(chan struct{})
	go func() { r.reapIdle(); close(reaped) }()
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		retiring := r.retiring[1] != nil
		r.mu.Unlock()
		if retiring {
			break
		}
		select {
		case <-deadline:
			t.Fatal("runtime did not begin retirement")
		case <-time.After(time.Millisecond):
		}
	}
	replacement := make(chan *tenantRuntime, 1)
	go func() { replacement <- r.acquire(1) }()
	select {
	case <-replacement:
		t.Fatal("replacement credential owner admitted before rotation persisted")
	case <-time.After(25 * time.Millisecond):
	}
	once.Do(func() { close(resume) })
	if err := <-rotation; err != nil {
		t.Fatal(err)
	}
	<-reaped
	next := <-replacement
	if next == nil || next == runtime {
		t.Fatal("retirement did not create a new runtime")
	}
	defer r.release(next)
	credential, err := next.accounts.Prepare(r.ctx, a.ID, func(context.Context, accounts.Credential) (accounts.Credential, error) {
		t.Error("replacement lost persisted rotation schedule")
		return accounts.Credential{}, accounts.ErrRefresh
	})
	if err != nil || credential.AccessToken != "synthetic-new" {
		t.Fatalf("replacement lost rotated credential: %v", err)
	}
}

func TestRegistryShutdownJoinsScheduledRotation(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r := newRefreshRegistry(t, func(*http.Request) (*http.Response, error) {
		close(entered)
		<-resume
		return syntheticRotationResponse(), nil
	})
	t.Cleanup(func() { once.Do(func() { close(resume) }) })
	runtime := r.acquire(1)
	a := authorizeDueAccount(t, r, runtime)
	r.release(runtime)
	// Exercise the real worker with a short synthetic interval, without waiting a production minute.
	r.refreshInterval = time.Millisecond
	r.Start()
	<-entered
	closed := make(chan struct{})
	go func() { r.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("registry closed with an unsaved rotation")
	case <-time.After(25 * time.Millisecond):
	}
	once.Do(func() { close(resume) })
	<-closed
	credential, err := accounts.NewForTenant(r.db, r.vault, 1).Prepare(context.Background(), a.ID, nil)
	if err != nil || credential.AccessToken != "synthetic-new" || credential.RefreshToken != "synthetic-rotated" {
		t.Fatalf("shutdown did not persist rotation: %v", err)
	}
	if r.acquire(1) != nil {
		t.Fatal("closed registry admitted new work")
	}
}
