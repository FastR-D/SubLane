package accounts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCodexRefreshLeadAndShortLivedRotation(t *testing.T) {
	for _, remaining := range []time.Duration{97 * time.Hour, 96 * time.Hour, time.Hour} {
		t.Run(remaining.String(), func(t *testing.T) {
			s, account, now := refreshFixture(t, "codex", remaining)
			s.ConfigureRefresh(context.Background(), 96*time.Hour)
			calls := 0
			refresh := func(_ context.Context, c Credential) (Credential, error) {
				calls++
				c.AccessToken, c.RefreshToken = "synthetic-new", "synthetic-next"
				c.ExpiresAt = now.Add(time.Hour).Unix()
				return c, nil
			}
			c, err := s.Prepare(context.Background(), account.ID, refresh)
			if err != nil || (remaining <= 96*time.Hour && calls != 1) || (remaining > 96*time.Hour && calls != 0) {
				t.Fatalf("refresh lead: calls=%d err=%v", calls, err)
			}
			if calls == 0 {
				return
			}
			if c.RefreshAfter != now.Add(30*time.Minute).Unix() {
				t.Fatal("short-lived token has no usable refresh interval")
			}
			restarted := New(s.db, s.vault)
			restarted.now = s.now
			restarted.ConfigureRefresh(context.Background(), 96*time.Hour)
			if _, err := restarted.Prepare(context.Background(), account.ID, nil); err != nil {
				t.Fatalf("restart repeated a fresh short-lived rotation: %v", err)
			}
		})
	}
}

func TestRefreshDoesNotBlockOtherAccounts(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Minute)
	other, err := s.Authorize(context.Background(), "Synthetic other", Credential{AccessToken: "synthetic-other", RefreshToken: "synthetic-refresh", AccountID: "synthetic-other", ExpiresAt: now.Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); <-done })
	go func() {
		_, err := s.Prepare(context.Background(), account.ID, func(_ context.Context, c Credential) (Credential, error) {
			close(started)
			<-release
			c.AccessToken, c.ExpiresAt = "synthetic-new", now.Add(time.Hour).Unix()
			return c, nil
		})
		done <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	otherDone := make(chan error, 1)
	go func() {
		_, err := s.Prepare(ctx, other.ID, nil)
		if err == nil {
			_, err = s.SetEnabled(ctx, other.ID, false)
		}
		otherDone <- err
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("unrelated account blocked: %v", err)
		}
	case <-ctx.Done():
		once.Do(func() { close(release) })
		<-otherDone
		t.Fatal("unrelated account waited for refresh IO")
	}
}

func TestRepeatedUnauthorizedRetainsRefreshRecovery(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Hour)
	if err := s.RecordUse(context.Background(), account.ID, "synthetic-old", false); err != nil {
		t.Fatal(err)
	}
	row, err := s.Get(context.Background(), account.ID)
	if err != nil || row.Status == "reauth_required" {
		t.Fatalf("business 401 permanently invalidated refresh credentials: %v", err)
	}
	if _, err := s.Prepare(context.Background(), account.ID, nil); !errors.Is(err, ErrRefresh) {
		t.Fatalf("rejected token bypassed refresh backoff: %v", err)
	}
	*now = now.Add(31 * time.Second)
	c, err := s.Prepare(context.Background(), account.ID, func(_ context.Context, c Credential) (Credential, error) {
		c.AccessToken, c.ExpiresAt = "synthetic-new", now.Add(time.Hour).Unix()
		return c, nil
	})
	if err != nil || c.AccessToken != "synthetic-new" {
		t.Fatalf("business 401 prevented automatic recovery: %v", err)
	}
	if err := s.RecordUse(context.Background(), account.ID, c.AccessToken, false); err != nil {
		t.Fatal(err)
	}
	_, err = s.Prepare(context.Background(), account.ID, nil)
	var retry *RefreshError
	if !errors.As(err, &retry) || retry.RetryAfter != 60 {
		t.Fatalf("token rotation reset repeated-401 backoff: %v", err)
	}
}

func TestImportedCredentialsCannotPostponeRefresh(t *testing.T) {
	c, err := ParseCredential([]byte(`{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","account_id":"synthetic-account","expires_at":2000000000,"refresh_after":9000000000}`))
	if err != nil || c.RefreshAfter != 0 {
		t.Fatalf("imported refresh policy was trusted: %v", err)
	}
}

func TestRefreshCloseJoinsRotationAndRejectsNewOnes(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Minute)
	s.ConfigureRefresh(context.Background(), 96*time.Hour)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := s.Prepare(context.Background(), account.ID, func(_ context.Context, c Credential) (Credential, error) {
			close(started)
			<-release
			c.AccessToken, c.ExpiresAt = "synthetic-new", now.Add(time.Hour).Unix()
			return c, nil
		})
		done <- err
	}()
	<-started
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("close returned before credential persistence")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-closed
	if _, err := s.RefreshAfterRejection(context.Background(), account.ID, "synthetic-new", func(context.Context, Credential) (Credential, error) {
		t.Error("closed service started a new rotation")
		return Credential{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed refresh owner accepted new work: %v", err)
	}
}
