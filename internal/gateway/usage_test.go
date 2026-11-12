package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

type quotaFixture struct {
	service           *Service
	accounts          *accounts.Service
	connection        *sql.DB
	path, keyPath, id string
	provider          string
	calls             atomic.Int32
	fail              atomic.Bool
	clock             atomic.Int64
	block             chan struct{}
	started           chan struct{}
}

func newQuotaFixture(t *testing.T) *quotaFixture {
	return newProviderQuotaFixture(t, "codex")
}

func newProviderQuotaFixture(t *testing.T, provider string) *quotaFixture {
	t.Helper()
	f := &quotaFixture{path: filepath.Join(t.TempDir(), "quota.db"), keyPath: filepath.Join(t.TempDir(), "key"), provider: provider}
	f.clock.Store(time.Now().Unix())
	f.open(t)
	identity, err := auth.New(f.connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(context.Background(), "synthetic-admin", "synthetic-password", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	row, err := f.accounts.Authorize(context.Background(), "Synthetic subscription", accounts.Credential{Provider: provider, AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", AccountID: "synthetic-account", ExpiresAt: time.Now().Add(24 * time.Hour).Unix(), Metadata: map[string]json.RawMessage{"project_id": json.RawMessage(`"synthetic-project"`)}}, "")
	if err != nil {
		t.Fatal(err)
	}
	f.id = row.ID
	configureTestPool(t, f.connection)
	t.Cleanup(func() { f.service.Close(); f.connection.Close() })
	return f
}
func (f *quotaFixture) open(t *testing.T) {
	t.Helper()
	var err error
	open := storage.Open
	if f.connection == nil {
		open = testdb.Open
	}
	f.connection, err = open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(f.keyPath, true)
	if err != nil {
		t.Fatal(err)
	}
	f.accounts = accounts.New(f.connection, cipher)
	client := upstream.NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/backend-api/wham/rate-limit-reset-credits" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`))}, nil
		}
		f.calls.Add(1)
		if f.started != nil {
			select {
			case f.started <- struct{}{}:
			default:
			}
		}
		if f.block != nil {
			select {
			case <-f.block:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		if f.fail.Load() {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"private":"synthetic-secret"}`))}, nil
		}
		body := `{"rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_at":2000000000}}}`
		if f.provider == "claude" {
			body = `{"five_hour":{"utilization":25,"resets_at":"2030-01-01T00:00:00Z"},"seven_day":null}`
		} else if f.provider == "xai" {
			body = `{"config":{"creditUsagePercent":25,"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2030-01-01T00:00:00Z"}}}`
		} else if f.provider == "antigravity" {
			body = `{"models":{"synthetic-model":{"quotaInfo":{"remainingFraction":0.75,"resetTime":"2030-01-01T00:00:00Z"}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	f.service = New(context.Background(), f.connection, f.accounts, client)
	f.service.usage.now = func() time.Time { return time.Unix(f.clock.Load(), 0) }
}

func TestSubscriptionUsageCachePersistsAndRetainsFailedRefresh(t *testing.T) {
	for _, provider := range []string{"claude", "antigravity", "xai"} {
		t.Run(provider, func(t *testing.T) {
			f := newProviderQuotaFixture(t, provider)
			first := waitQuota(t, f)
			if first.Stale || len(first.Limits) != 1 || first.Limits[0].Windows[0].UsedPercent == nil || *first.Limits[0].Windows[0].UsedPercent != 25 {
				t.Fatal("provider usage did not reach the shared cache")
			}
			f.service.Close()
			f.connection.Close()
			f.open(t)
			next, err := f.service.Usage(context.Background(), f.id)
			if err != nil || next.UpdatedAt != first.UpdatedAt || f.calls.Load() != 1 {
				t.Fatalf("provider snapshot did not survive restart: %v", err)
			}
			f.clock.Add(121)
			f.fail.Store(true)
			failed := waitQuota(t, f)
			if !failed.Stale || !failed.RefreshFailed || failed.UpdatedAt != first.UpdatedAt || *failed.Limits[0].Windows[0].UsedPercent != 25 {
				t.Fatal("failed quota read discarded the previous observation")
			}
			if _, err := f.accounts.SetEnabled(context.Background(), f.id, false); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.Usage(context.Background(), f.id); !errors.Is(err, accounts.ErrDisabled) {
				t.Fatal("disabled provider account served cached usage")
			}
		})
	}
}

func TestSubscriptionQuotaWarmupSharesBoundedWorkers(t *testing.T) {
	for _, provider := range []string{"claude", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			f := newProviderQuotaFixture(t, provider)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for _, id := range []string{"synthetic-second", "synthetic-third"} {
				if _, err := f.accounts.Authorize(ctx, id, accounts.Credential{Provider: provider, AccountID: id, AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix(), Metadata: map[string]json.RawMessage{"project_id": json.RawMessage(`"synthetic-project"`)}}, ""); err != nil {
					t.Fatal(err)
				}
			}
			configureTestPool(t, f.connection)
			f.block = make(chan struct{})
			f.started = make(chan struct{}, 3)
			defer close(f.block)
			if err := f.service.warmUsage(ctx, 1, 1, provider); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				select {
				case <-f.started:
				case <-ctx.Done():
					t.Fatal("subscription traffic did not warm quota asynchronously")
				}
			}
			if err := f.service.warmUsage(ctx, 1, 1, provider); err != nil {
				t.Fatal(err)
			}
			if f.calls.Load() != 2 || len(f.service.usage.slots) != 2 || len(f.service.slots) != 2 {
				t.Fatal("subscription reads bypassed shared bounds or single-flight", f.calls.Load())
			}
		})
	}
}
func waitQuota(t *testing.T, f *quotaFixture) UsageSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := f.service.RefreshUsage(ctx, f.id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestUsageSnapshotSurvivesDatabaseReopen(t *testing.T) {
	f := newQuotaFixture(t)
	first := waitQuota(t, f)
	if first.Stale || first.Refreshing || first.UpdatedAt == 0 {
		t.Fatal("invalid fresh snapshot", first)
	}
	f.service.Close()
	f.connection.Close()
	f.open(t)
	next, err := f.service.Usage(context.Background(), f.id)
	if err != nil || next.UpdatedAt != first.UpdatedAt || f.calls.Load() != 1 || len(next.Limits) != 1 || next.ResetCredits == nil || *next.ResetCredits != 2 {
		t.Fatalf("restart lost snapshot: %+v %v calls=%d", next, err, f.calls.Load())
	}
	if *next.Limits[0].Windows[0].UsedPercent != 25 {
		t.Fatal("wrong recovered usage")
	}
}
func TestUsageReturnsStaleSnapshotWhileRefreshingAndCoalescesCallers(t *testing.T) {
	f := newQuotaFixture(t)
	first := waitQuota(t, f)
	f.clock.Add(121)
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	stale, err := f.service.Usage(context.Background(), f.id)
	if err != nil || !stale.Stale || !stale.Refreshing || stale.UpdatedAt != first.UpdatedAt {
		t.Fatal("stale snapshot blocked or disappeared", err, stale)
	}
	<-f.started
	for range 12 {
		value, err := f.service.Usage(context.Background(), f.id)
		if err != nil || !value.Refreshing {
			t.Fatal("parallel reader lost stale snapshot", err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.service.RefreshUsage(canceled, f.id); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled waiter did not exit", err)
	}
	if f.calls.Load() != 2 {
		t.Fatal("duplicate upstream requests", f.calls.Load())
	}
	close(f.block)
	fresh := waitQuota(t, f)
	if fresh.Stale || fresh.UpdatedAt <= first.UpdatedAt || f.calls.Load() != 2 {
		t.Fatal("refresh not published", fresh)
	}
}
func TestUsageRefreshFailurePreservesSnapshotAndBacksOff(t *testing.T) {
	f := newQuotaFixture(t)
	first := waitQuota(t, f)
	waitQuota(t, f)
	if f.calls.Load() != 1 {
		t.Fatal("manual refresh ignored cooldown")
	}
	f.clock.Add(6)
	f.fail.Store(true)
	failed := waitQuota(t, f)
	if !failed.RefreshFailed || failed.RefreshError != "upstream_unavailable" || !failed.Stale || failed.UpdatedAt != first.UpdatedAt || failed.RetryAfterSeconds <= 0 {
		t.Fatal("failed refresh discarded last good snapshot", failed)
	}
	waitQuota(t, f)
	if f.calls.Load() != 2 {
		t.Fatal("failure hammered upstream")
	}
	f.clock.Add(31)
	f.fail.Store(false)
	if value := waitQuota(t, f); value.Stale || value.RefreshFailed || value.UpdatedAt <= first.UpdatedAt {
		t.Fatal("recovery failed", value)
	}
}

func TestUsageRefreshErrorCodeClassifiesSafeReasons(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "rate limited", err: &upstream.UpstreamError{Status: 429}, want: "rate_limited"},
		{name: "reauthorization", err: accounts.ErrReauthorize, want: "reauthorization_required"},
		{name: "credential refresh", err: accounts.ErrRefresh, want: "credential_refresh_failed"},
		{name: "unsupported", err: upstream.ErrUsageUnsupported, want: "provider_usage_unsupported"},
		{name: "response", err: upstream.ErrResponse, want: "invalid_upstream_response"},
		{name: "upstream", err: upstream.ErrUpstream, want: "upstream_unavailable"},
		{name: "unknown", err: errors.New("synthetic failure"), want: "refresh_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usageRefreshError(tt.err); got != tt.want {
				t.Fatalf("usageRefreshError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
func TestUsageCacheCannotBypassAccountDisableOrDeletion(t *testing.T) {
	f := newQuotaFixture(t)
	waitQuota(t, f)
	if _, err := f.accounts.SetEnabled(context.Background(), f.id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Usage(context.Background(), f.id); !errors.Is(err, accounts.ErrDisabled) {
		t.Fatal("disabled account served cached quota", err)
	}
	if err := f.accounts.Delete(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Usage(context.Background(), f.id); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("deleted account served cached quota", err)
	}
	var count int
	if err := f.connection.QueryRow("SELECT count(*) FROM account_usage").Scan(&count); err != nil || count != 0 {
		t.Fatal("orphaned snapshot", count, err)
	}
}
func TestUsageShutdownCancelsBackgroundRefresh(t *testing.T) {
	f := newQuotaFixture(t)
	waitQuota(t, f)
	f.clock.Add(121)
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	if _, err := f.service.Usage(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	<-f.started
	done := make(chan struct{})
	go func() { f.service.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown left upstream work running")
	}
}

func TestUsageRestartReturnsExpiredSnapshotBeforeFailedRefresh(t *testing.T) {
	f := newQuotaFixture(t)
	first := waitQuota(t, f)
	f.service.Close()
	f.connection.Close()
	f.clock.Add(121)
	f.fail.Store(true)
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	f.open(t)
	saved, err := f.service.Usage(context.Background(), f.id)
	if err != nil || !saved.Stale || !saved.Refreshing || saved.UpdatedAt != first.UpdatedAt {
		t.Fatal("reboot did not restore expired snapshot immediately", saved, err)
	}
	<-f.started
	close(f.block)
	failed := waitQuota(t, f)
	if !failed.RefreshFailed || failed.UpdatedAt != first.UpdatedAt {
		t.Fatal("failed restart refresh lost previous observation", failed)
	}
}

func TestUsageColdFailureIsCoalescedAndCached(t *testing.T) {
	f := newQuotaFixture(t)
	f.fail.Store(true)
	if _, err := f.service.Usage(context.Background(), f.id); err == nil {
		t.Fatal("missing quota fabricated on failure")
	}
	if _, err := f.service.RefreshUsage(context.Background(), f.id); err == nil || f.calls.Load() != 1 {
		t.Fatal("cold failure was not backed off", err, f.calls.Load())
	}
}

func TestUsageRefreshCannotPublishAcrossAccountLifecycle(t *testing.T) {
	f := newQuotaFixture(t)
	first := waitQuota(t, f)
	f.clock.Add(121)
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 1)
	if _, err := f.service.Usage(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	<-f.started
	if _, err := f.accounts.SetEnabled(context.Background(), f.id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.SetEnabled(context.Background(), f.id, true); err != nil {
		t.Fatal(err)
	}
	close(f.block)
	f.service.Close()
	var observed int64
	if err := f.connection.QueryRow("SELECT updated_at FROM account_usage WHERE account_id=?", f.id).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	if observed != first.UpdatedAt {
		t.Fatal("in-flight quota crossed lifecycle revision")
	}
}

func TestTrafficQuotaRefreshDoesNotWaitAndBoundsWorkers(t *testing.T) {
	f := newQuotaFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, id := range []string{"synthetic-second", "synthetic-third"} {
		if _, err := f.accounts.Authorize(ctx, id, accounts.Credential{AccountID: id, AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, ""); err != nil {
			t.Fatal(err)
		}
	}
	configureTestPool(t, f.connection)
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 3)
	defer close(f.block)
	if err := f.service.warmUsage(ctx, 1, 1, "codex"); err != nil {
		t.Fatal("metadata blocked model admission", err)
	}
	for range 2 {
		select {
		case <-f.started:
		case <-ctx.Done():
			t.Fatal("quota workers did not start")
		}
	}
	if err := f.service.warmUsage(ctx, 1, 1, "codex"); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 2 || len(f.service.usage.slots) != 2 {
		t.Fatal("unbounded or duplicate refresh", f.calls.Load())
	}
	if len(f.service.slots) != 2 {
		t.Fatal("quota did not share upstream admission")
	}
}
