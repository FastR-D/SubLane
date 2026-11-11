package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/vault"
)

func refreshFixture(t *testing.T, provider string, validity time.Duration) (*Service, Account, *time.Time) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	connection, err := storage.Open(ctx, filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	seedInitialTenant(t, connection)
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	s := New(connection, cipher)
	now := time.Unix(2_000_000_000, 0)
	s.now = func() time.Time { return now }
	account, err := s.Authorize(ctx, "Synthetic subscription", Credential{Provider: provider, AccessToken: "synthetic-old", RefreshToken: "synthetic-refresh", AccountID: "synthetic-subject", ExpiresAt: now.Add(validity).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, account, &now
}

func TestRefreshFailureUsesValidTokenAndRecoversAfterBackoff(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", 90*time.Second)
	calls := 0
	failing := true
	refresh := func(_ context.Context, c Credential) (Credential, error) {
		calls++
		if failing {
			return Credential{}, errors.New("synthetic timeout")
		}
		c.AccessToken, c.RefreshToken = "synthetic-new", "synthetic-rotated"
		c.ExpiresAt = now.Add(time.Hour).Unix()
		return c, nil
	}
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			c, err := s.Prepare(context.Background(), account.ID, refresh)
			if err != nil || c.AccessToken != "synthetic-old" {
				t.Errorf("temporary failure blocked a valid token: %v", err)
			}
		})
	}
	callers.Wait()
	if calls != 1 {
		t.Fatalf("concurrent callers repeated failed refresh: %d", calls)
	}
	*now = now.Add(31 * time.Second)
	if _, err := s.Prepare(context.Background(), account.ID, refresh); err != nil || calls != 2 {
		t.Fatalf("refresh was not retried after backoff: calls=%d err=%v", calls, err)
	}
	*now = now.Add(30 * time.Second)
	if c, err := s.Prepare(context.Background(), account.ID, refresh); !errors.Is(err, ErrRefresh) || c.AccessToken != "" || calls != 2 {
		t.Fatalf("backoff bypassed expiry safety margin: calls=%d err=%v", calls, err)
	}
	failing = false
	*now = now.Add(31 * time.Second)
	c, err := s.Prepare(context.Background(), account.ID, refresh)
	if err != nil || c.AccessToken != "synthetic-new" || calls != 3 {
		t.Fatalf("refresh did not recover automatically: calls=%d err=%v", calls, err)
	}
	if c, err := s.Prepare(context.Background(), account.ID, nil); err != nil || c.AccessToken != "synthetic-new" {
		t.Fatalf("recovered credential was not persisted: %v", err)
	}
}

func TestRefreshRejectionSurvivesLaterRequestsAndRestart(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Hour)
	ctx := context.Background()
	calls := 0
	fail := func(context.Context, Credential) (Credential, error) {
		calls++
		return Credential{}, errors.New("synthetic outage")
	}
	if _, err := s.RefreshAfterRejection(ctx, account.ID, "synthetic-old", fail); !errors.Is(err, ErrRefresh) {
		t.Fatalf("rejected token fell back: %v", err)
	}
	if err := s.RecordUse(ctx, account.ID, "synthetic-old", true); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Prepare(ctx, account.ID, fail); !errors.Is(err, ErrRefresh) || c.AccessToken != "" || calls != 1 {
		t.Fatalf("later request reused rejected token or bypassed backoff: calls=%d err=%v", calls, err)
	}
	restarted := New(s.db, s.vault)
	restarted.now = s.now
	if c, err := restarted.Prepare(ctx, account.ID, fail); !errors.Is(err, ErrRefresh) || c.AccessToken != "" {
		t.Fatalf("restart reused rejected token: %v", err)
	}
	*now = now.Add(31 * time.Second)
	c, err := restarted.Prepare(ctx, account.ID, func(_ context.Context, c Credential) (Credential, error) {
		c.AccessToken = "synthetic-new"
		c.ExpiresAt = now.Add(time.Hour).Unix()
		return c, nil
	})
	if err != nil || c.AccessToken != "synthetic-new" {
		t.Fatalf("rejected token could not recover: %v", err)
	}
	if c, err := restarted.RefreshAfterRejection(ctx, account.ID, "synthetic-old", nil); err != nil || c.AccessToken != "synthetic-new" {
		t.Fatalf("late rejection invalidated the rotated token: %v", err)
	}
}

func TestRefreshPersistsRotationWhenCallerCancels(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	credential, err := s.Prepare(ctx, account.ID, func(refreshCtx context.Context, c Credential) (Credential, error) {
		// The provider has consumed the old refresh token before the caller disconnects.
		cancel()
		if err := refreshCtx.Err(); err != nil {
			t.Errorf("caller cancellation interrupted credential rotation: %v", err)
		}
		if deadline, ok := refreshCtx.Deadline(); !ok || time.Until(deadline) > time.Minute {
			t.Error("credential rotation must have an independent bounded deadline")
		}
		c.AccessToken, c.RefreshToken = "synthetic-new", "synthetic-rotated"
		c.ExpiresAt = now.Add(time.Hour).Unix()
		return c, nil
	})
	if !errors.Is(err, context.Canceled) || credential.AccessToken != "" {
		t.Fatalf("canceled caller received a usable credential: %v", err)
	}
	restarted := New(s.db, s.vault)
	restarted.now = s.now
	c, err := restarted.Prepare(context.Background(), account.ID, nil)
	if err != nil || c.AccessToken != "synthetic-new" || c.RefreshToken != "synthetic-rotated" {
		t.Fatalf("caller cancellation lost the durable rotation: %v", err)
	}
}

func TestRefreshPersistsReauthorizationWhenCallerCancels(t *testing.T) {
	s, account, _ := refreshFixture(t, "codex", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		cancel()
		return Credential{}, ErrReauthorize
	})
	if !errors.Is(err, ErrReauthorize) {
		t.Fatalf("permanent credential failure was lost: %v", err)
	}
	saved, err := s.Get(context.Background(), account.ID)
	if err != nil || saved.Status != "reauth_required" {
		t.Fatalf("caller cancellation lost permanent credential failure: status=%s err=%v", saved.Status, err)
	}
}

func TestRefreshDoesNotStartForCanceledCaller(t *testing.T) {
	s, account, _ := refreshFixture(t, "codex", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		t.Fatal("canceled caller started credential rotation")
		return Credential{}, nil
	})
	if !errors.Is(err, context.Canceled) || c.AccessToken != "" {
		t.Fatalf("canceled caller received a usable credential: %v", err)
	}
}

func TestRefreshFallbackPreservesCredentialSafeguards(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		validity       time.Duration
		failure        error
		want           error
	}{
		{"expired", "codex", -time.Second, errors.New("synthetic timeout"), ErrRefresh},
		{"expiry margin", "codex", 30 * time.Second, errors.New("synthetic timeout"), ErrRefresh},
		{"reauthorization", "codex", time.Minute, ErrReauthorize, ErrReauthorize},
		{"claude", "claude", time.Minute, errors.New("synthetic timeout"), ErrRefresh},
		{"antigravity", "antigravity", 10 * time.Minute, errors.New("synthetic timeout"), ErrRefresh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, account, _ := refreshFixture(t, tc.provider, tc.validity)
			c, err := s.Prepare(context.Background(), account.ID, func(context.Context, Credential) (Credential, error) { return Credential{}, tc.failure })
			if !errors.Is(err, tc.want) || c.AccessToken != "" {
				t.Fatalf("unsafe fallback: %v", err)
			}
		})
	}
}

func TestRefreshFallbackRechecksTimeAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "elapsed", true: "canceled"}[canceled], func(t *testing.T) {
			s, account, now := refreshFixture(t, "codex", time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, err := s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
				if canceled {
					cancel()
				} else {
					*now = now.Add(31 * time.Second)
				}
				return Credential{}, errors.New("synthetic failure")
			})
			want := ErrRefresh
			if canceled {
				want = context.Canceled
			}
			if !errors.Is(err, want) || c.AccessToken != "" {
				t.Fatalf("failed to recheck after refresh: %v", err)
			}
		})
	}
}

func TestReauthorizationClearsRefreshBackoff(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", -time.Second)
	ctx := context.Background()
	_, _ = s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		return Credential{}, errors.New("synthetic timeout")
	})
	if _, err := s.Authorize(ctx, "Synthetic repaired", Credential{AccessToken: "synthetic-repaired", RefreshToken: "synthetic-new-refresh", AccountID: "synthetic-subject", ExpiresAt: now.Add(time.Minute).Unix()}, account.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c, err := s.Prepare(ctx, account.ID, func(_ context.Context, c Credential) (Credential, error) {
		calls++
		c.AccessToken = "synthetic-renewed"
		c.ExpiresAt = now.Add(time.Hour).Unix()
		return c, nil
	})
	if err != nil || calls != 1 || c.AccessToken != "synthetic-renewed" {
		t.Fatalf("reauthorization retained old backoff: calls=%d err=%v", calls, err)
	}
}

func TestRefreshBackoffIsBoundedAndHonorsUpstreamRetry(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", -time.Second)
	ctx := context.Background()
	for _, seconds := range []int64{30, 60, 120, 240, 300, 300} {
		_, err := s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
			return Credential{}, errors.New("synthetic outage")
		})
		var retry *RefreshError
		if !errors.As(err, &retry) || retry.RetryAfter != seconds {
			t.Fatalf("expected %d seconds backoff: %v", seconds, err)
		}
		*now = now.Add(time.Duration(seconds) * time.Second)
	}
	_, err := s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		return Credential{}, &RefreshError{RetryAfter: 900}
	})
	var retry *RefreshError
	if !errors.As(err, &retry) || retry.RetryAfter != 900 {
		t.Fatalf("upstream retry was shortened: %v", err)
	}
	*now = now.Add(100 * time.Second)
	_, err = s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		t.Fatal("upstream retry was bypassed")
		return Credential{}, nil
	})
	if !errors.As(err, &retry) || retry.RetryAfter != 800 {
		t.Fatalf("retry countdown was extended: %v", err)
	}
}

func TestRefreshBackoffDoesNotBlockOtherAccountsOrBypassDisable(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Minute)
	ctx := context.Background()
	_, _ = s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		return Credential{}, errors.New("synthetic timeout")
	})
	other, err := s.Authorize(ctx, "Synthetic second", Credential{AccessToken: "synthetic-second", RefreshToken: "synthetic-refresh", AccountID: "synthetic-second", ExpiresAt: now.Add(time.Minute).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, err := s.Prepare(ctx, other.ID, func(_ context.Context, c Credential) (Credential, error) {
		calls++
		c.ExpiresAt = now.Add(time.Hour).Unix()
		return c, nil
	}); err != nil || calls != 1 {
		t.Fatalf("backoff affected another account: calls=%d err=%v", calls, err)
	}
	if _, err := s.SetEnabled(ctx, account.ID, false); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Prepare(ctx, account.ID, nil); !errors.Is(err, ErrDisabled) || c.AccessToken != "" {
		t.Fatalf("fallback bypassed disable: %v", err)
	}
}

func TestRefreshFallbackCannotHideCredentialPersistenceFailure(t *testing.T) {
	s, account, now := refreshFixture(t, "codex", time.Minute)
	if _, err := s.db.Exec("CREATE TRIGGER reject_refresh BEFORE UPDATE OF credential ON accounts BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END"); err != nil {
		t.Fatal(err)
	}
	c, err := s.Prepare(context.Background(), account.ID, func(_ context.Context, c Credential) (Credential, error) {
		c.AccessToken, c.RefreshToken = "synthetic-new", "synthetic-rotated"
		c.ExpiresAt = now.Add(time.Hour).Unix()
		return c, nil
	})
	if err == nil || c.AccessToken != "" {
		t.Fatal("persistence failure returned usable credentials")
	}
}

func TestRefreshLateSuccessCannotClearReauthorization(t *testing.T) {
	s, account, _ := refreshFixture(t, "codex", time.Minute)
	ctx := context.Background()
	_, err := s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) { return Credential{}, ErrReauthorize })
	if !errors.Is(err, ErrReauthorize) {
		t.Fatal(err)
	}
	if err := s.RecordUse(ctx, account.ID, "synthetic-old", true); err != nil {
		t.Fatal(err)
	}
	account, err = s.Get(ctx, account.ID)
	if err != nil || account.Status != "reauth_required" {
		t.Fatalf("late success cleared permanent credential failure: status=%s err=%v", account.Status, err)
	}
}

func TestRefreshInvalidResultCannotFallbackOrRepeatImmediately(t *testing.T) {
	for _, invalid := range []string{"short expiry", "same rejected token", "identity", "persistence"} {
		t.Run(invalid, func(t *testing.T) {
			s, account, now := refreshFixture(t, "codex", time.Minute)
			ctx := context.Background()
			if invalid == "persistence" {
				if _, err := s.db.Exec("CREATE TRIGGER reject_refresh BEFORE UPDATE OF credential ON accounts BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END"); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			refresh := func(_ context.Context, c Credential) (Credential, error) {
				calls++
				if invalid != "same rejected token" {
					c.AccessToken = "synthetic-new"
				}
				if invalid != "short expiry" {
					c.ExpiresAt = now.Add(time.Hour).Unix()
				}
				if invalid == "identity" {
					c.AccountID = "synthetic-other"
				}
				return c, nil
			}
			var c Credential
			var err error
			if invalid == "same rejected token" {
				c, err = s.RefreshAfterRejection(ctx, account.ID, "synthetic-old", refresh)
			} else {
				c, err = s.Prepare(ctx, account.ID, refresh)
			}
			if err == nil || c.AccessToken != "" {
				t.Fatal("invalid refresh returned a usable token")
			}
			c, err = s.Prepare(ctx, account.ID, refresh)
			if err == nil || c.AccessToken != "" || calls != 1 {
				t.Fatalf("invalid refresh bypassed backoff or fell back: calls=%d err=%v", calls, err)
			}
		})
	}
}
