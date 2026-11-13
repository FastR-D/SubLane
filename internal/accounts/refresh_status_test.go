package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCredentialRefreshStatusTracksFailureRecoveryAndRestart(t *testing.T) {
	ctx := context.Background()
	s, account, now := refreshFixture(t, "codex", time.Hour)
	s.ConfigureRefresh(ctx, CodexRefreshLead)
	if _, err := s.SetEnabled(ctx, account.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshDue(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		return Credential{}, &RefreshFailure{Status: 503, Code: "synthetic-private-code", Err: errors.New("synthetic-private-body")}
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := s.List(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatal(err)
	}
	status := listed[0].CredentialRefresh
	if status == nil || !status.Failed || status.Refreshing || status.RetryAt != now.Add(30*time.Second).Unix() || status.LastFailureAt != now.Unix() || status.ServerTime != now.Unix() {
		t.Fatalf("missing actual refresh failure: %+v", status)
	}
	if listed[0].ExpiresAt <= status.ServerTime || listed[0].Enabled {
		t.Fatal("early refresh failure changed expiry or enablement")
	}
	raw, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-private", "synthetic-old", "synthetic-refresh"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("refresh status leaked secret material")
		}
	}
	restarted := New(s.db, s.vault)
	restarted.now = s.now
	afterRestart, err := restarted.Get(ctx, account.ID)
	if err != nil || afterRestart.CredentialRefresh == nil || afterRestart.CredentialRefresh.Failed || afterRestart.CredentialRefresh.RetryAt != 0 {
		t.Fatal("restart invented a current failure", err)
	}
	*now = now.Add(31 * time.Second)
	if err := s.RefreshDue(ctx, account.ID, func(_ context.Context, c Credential) (Credential, error) {
		c.AccessToken = "synthetic-new"
		c.ExpiresAt = now.Add(10 * 24 * time.Hour).Unix()
		return c, nil
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Get(ctx, account.ID)
	if err != nil || recovered.CredentialRefresh.Failed || recovered.CredentialRefresh.Refreshing || recovered.CredentialRefresh.RetryAt != 0 || recovered.CredentialRefresh.LastFailureAt != 0 || recovered.Enabled {
		t.Fatal("saved recovery retained a stale warning", err)
	}
}

func TestCredentialRefreshStatusDoesNotBlockDuringProviderIO(t *testing.T) {
	ctx := context.Background()
	s, account, now := refreshFixture(t, "codex", time.Minute)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- s.RefreshDue(ctx, account.ID, func(_ context.Context, c Credential) (Credential, error) {
			close(entered)
			<-release
			c.AccessToken = "synthetic-new"
			c.ExpiresAt = now.Add(time.Hour).Unix()
			return c, nil
		})
	}()
	<-entered
	row, err := s.Get(ctx, account.ID)
	close(release)
	if refreshErr := <-done; refreshErr != nil {
		t.Fatal(refreshErr)
	}
	if err != nil || row.CredentialRefresh == nil || !row.CredentialRefresh.Refreshing || row.CredentialRefresh.Failed {
		t.Fatal("in-flight rotation was not observable", err)
	}
	row, err = s.Get(ctx, account.ID)
	if err != nil || row.CredentialRefresh.Refreshing {
		t.Fatal("completed rotation remained active", err)
	}
}

func TestCredentialRefreshStatusSeparatesExpiredAndPermanentFailure(t *testing.T) {
	ctx := context.Background()
	s, account, now := refreshFixture(t, "codex", -time.Second)
	if _, err := s.SetEnabled(ctx, account.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshDue(ctx, account.ID, func(context.Context, Credential) (Credential, error) { return Credential{}, ErrRefresh }); !errors.Is(err, ErrRefresh) {
		t.Fatal(err)
	}
	row, err := s.Get(ctx, account.ID)
	if err != nil || !row.CredentialRefresh.Failed || row.ExpiresAt > row.CredentialRefresh.ServerTime || row.Status == "reauth_required" {
		t.Fatal("expired transient failure became permanent", err)
	}
	*now = now.Add(time.Minute)
	if err := s.RefreshDue(ctx, account.ID, func(context.Context, Credential) (Credential, error) { return Credential{}, ErrReauthorize }); !errors.Is(err, ErrReauthorize) {
		t.Fatal(err)
	}
	row, err = s.Get(ctx, account.ID)
	if err != nil || row.Status != "reauth_required" || row.Enabled || row.CredentialRefresh.Failed || row.CredentialRefresh.RetryAt != 0 {
		t.Fatal("permanent rejection retained automatic retry", err)
	}
}
