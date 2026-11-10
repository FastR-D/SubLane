package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	storedb "github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

const usageTTL = 2 * time.Minute
const usageCooldown = 5 * time.Second

var errUsageChanged = errors.New("account_usage_changed")

// UsageSnapshot separates the observation time from the response clock and refresh state.
type UsageSnapshot struct {
	upstream.Usage
	ServerTime        int64  `json:"server_time"`
	ExpiresAt         int64  `json:"expires_at"`
	Stale             bool   `json:"stale"`
	Refreshing        bool   `json:"refreshing"`
	RefreshFailed     bool   `json:"refresh_failed"`
	RefreshError      string `json:"refresh_error,omitempty"`
	RetryAfterSeconds int64  `json:"retry_after_seconds"`
}

type usageEntry struct {
	revision int64
	snapshot *upstream.Usage
	flight   chan struct{}
	retryAt  time.Time
	err      error
	accessed time.Time
}

type usageCache struct {
	mu      sync.Mutex
	entries map[string]*usageEntry
	ctx     context.Context
	cancel  context.CancelFunc
	now     func() time.Time
	workers sync.WaitGroup
	closed  bool
	slots   chan struct{}
}

func newUsageCache(parent context.Context) *usageCache {
	ctx, cancel := context.WithCancel(parent)
	return &usageCache{entries: make(map[string]*usageEntry), slots: make(chan struct{}, 2), ctx: ctx, cancel: cancel, now: time.Now}
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.stopRuntime()
	s.mu.Unlock()
	s.workers.Wait()
	s.catalog.close()
	c := s.usage
	c.mu.Lock()
	// Close admission before waiting: no WaitGroup.Add may race with shutdown's Wait.
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.workers.Wait()
}

func (s *Service) Usage(ctx context.Context, id string) (UsageSnapshot, error) {
	return s.usageSnapshot(ctx, id, false, true)
}

func (s *Service) RefreshUsage(ctx context.Context, id string) (UsageSnapshot, error) {
	return s.usageSnapshot(ctx, id, true, true)
}

func (s *Service) usageAccount(ctx context.Context, id string) error {
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return err
	}
	if !account.Enabled {
		return accounts.ErrDisabled
	}
	if account.Status == "reauth_required" {
		return accounts.ErrReauthorize
	}
	if !accounts.ValidProvider(account.Provider) {
		return upstream.ErrUsageUnsupported
	}
	if !s.accounts.ProviderEnabled(account.Provider) {
		return accounts.ErrProviderDisabled
	}
	return nil
}

func (s *Service) usageSnapshot(ctx context.Context, id string, force, wait bool) (UsageSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return UsageSnapshot{}, err
	}
	// Cached quota must never bypass current account enablement or deletion.
	if err := s.usageAccount(ctx, id); err != nil {
		return UsageSnapshot{}, err
	}
	c := s.usage
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return UsageSnapshot{}, context.Canceled
	}
	now := c.now()
	e, err := s.loadUsage(ctx, id, now)
	if err != nil {
		c.mu.Unlock()
		return UsageSnapshot{}, err
	}
	current := snapshotState(e, now)
	if e.flight == nil && (force || e.snapshot == nil || current.Stale) && !now.Before(e.retryAt) {
		release, admissionErr := s.acquireUsage()
		if admissionErr != nil {
			e.err = admissionErr
			e.retryAt = now.Add(usageCooldown)
		} else {
			e.flight = make(chan struct{})
			e.retryAt = now.Add(usageCooldown)
			c.workers.Add(1)
			go s.fetchUsage(id, e, release)
		}
	}
	flight := e.flight
	if !wait || e.snapshot != nil && (!force || flight == nil) {
		current = snapshotState(e, now)
		c.mu.Unlock()
		return current, nil
	}
	if flight == nil {
		err = e.err
		c.mu.Unlock()
		if err == nil {
			err = upstream.ErrResponse
		}
		return UsageSnapshot{}, err
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return UsageSnapshot{}, ctx.Err()
	case <-flight:
	}
	if err := s.usageAccount(ctx, id); err != nil {
		return UsageSnapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, err = s.loadUsage(ctx, id, c.now())
	if err != nil {
		return UsageSnapshot{}, err
	}
	if e.snapshot != nil {
		return snapshotState(e, c.now()), nil
	}
	if e.err == nil {
		return UsageSnapshot{}, errUsageChanged
	}
	return UsageSnapshot{}, e.err
}

// Called under the cache lock; only small SQLite reads occur here, never provider IO.
func (s *Service) loadUsage(ctx context.Context, id string, now time.Time) (*usageEntry, error) {
	row, err := s.queries.GetAccountUsage(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.usage.load(id, row, now)
}

// Caller holds c.mu. Batched warmup and individual readers share decoded observations and eviction.
func (c *usageCache) load(id string, row storedb.GetAccountUsageRow, now time.Time) (*usageEntry, error) {
	if entry := c.entries[id]; entry != nil && entry.revision == row.Revision {
		entry.accessed = now
		return entry, nil
	}
	if len(c.entries) >= 100 {
		oldestID := ""
		var oldest time.Time
		for key, entry := range c.entries {
			if entry.flight == nil && (oldestID == "" || entry.accessed.Before(oldest)) {
				oldestID, oldest = key, entry.accessed
			}
		}
		if oldestID == "" {
			return nil, ErrBusy
		}
		delete(c.entries, oldestID)
	}
	entry := &usageEntry{accessed: now, revision: row.Revision, snapshot: savedUsage(row)}
	if entry.snapshot != nil {
		entry.retryAt = time.Unix(entry.snapshot.UpdatedAt, 0).Add(usageCooldown)
	}
	c.entries[id] = entry
	return entry, nil
}

func (s *Service) fetchUsage(id string, e *usageEntry, release func()) {
	c := s.usage
	defer c.workers.Done()
	defer release()
	// One browser leaving must not cancel a refresh shared by other readers. Process shutdown still cancels it.
	ctx, cancel := context.WithTimeout(c.ctx, 45*time.Second)
	defer cancel()
	readStartedAt := c.now().UnixMilli()
	value, err := readAccount(ctx, s, id, s.provider.Usage)
	value.ReadStartedAt = readStartedAt
	if err == nil {
		err = s.usageAccount(ctx, id)
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// A poll started before a response observation must not overwrite it when
	// network latency makes the older poll finish last.
	if err == nil && e.snapshot != nil && e.snapshot.ReadStartedAt > value.ReadStartedAt {
		e.retryAt = now.Add(usageCooldown)
		close(e.flight)
		e.flight = nil
		return
	}
	if err == nil {
		value.UpdatedAt = now.Unix()
		var raw []byte
		raw, err = json.Marshal(value)
		if err == nil {
			var n int64
			n, err = s.queries.SaveAccountUsage(ctx, storedb.SaveAccountUsageParams{AccountID: id, Snapshot: raw, UpdatedAt: value.UpdatedAt, Revision: e.revision})
			if err == nil && n == 0 {
				err = errUsageChanged
			}
		}
	}
	e.err = err
	e.retryAt = now.Add(usageCooldown)
	if err == nil {
		// Publish only after SQLite commits so a restart cannot lose an acknowledged observation.
		e.snapshot = &value
		s.recordSchedulingQuota(id, e.revision, value)
	} else {
		backoff := 30 * time.Second
		var upstream *upstream.UpstreamError
		if errors.As(err, &upstream) {
			if seconds, parseErr := strconv.Atoi(upstream.RetryAfter); parseErr == nil && seconds > 30 && seconds <= 3600 {
				backoff = time.Duration(seconds) * time.Second
			}
		}
		e.retryAt = now.Add(backoff)
	}
	close(e.flight)
	e.flight = nil
}

func snapshotState(e *usageEntry, now time.Time) UsageSnapshot {
	result := UsageSnapshot{ServerTime: now.Unix(), Refreshing: e.flight != nil, RefreshFailed: e.err != nil, RetryAfterSeconds: max(0, int64(math.Ceil(e.retryAt.Sub(now).Seconds())))}
	if e.err != nil {
		result.RefreshError = usageRefreshError(e.err)
	}
	if e.snapshot == nil {
		return result
	}
	result.Usage = *e.snapshot
	result.ExpiresAt = result.UpdatedAt + int64(usageTTL.Seconds())
	for _, limit := range result.Limits {
		for _, window := range limit.Windows {
			if window.ResetAt != nil && *window.ResetAt > result.UpdatedAt && *window.ResetAt < result.ExpiresAt {
				result.ExpiresAt = *window.ResetAt
			}
		}
	}
	result.Stale = e.err != nil || now.Unix() >= result.ExpiresAt || now.Unix() < result.UpdatedAt
	// Hidden limits still govern admission and freshness; filtering must not mutate the shared snapshot.
	result.Limits = slices.DeleteFunc(slices.Clone(result.Limits), func(limit upstream.UsageLimit) bool { return limit.Hidden })
	return result
}

// usageRefreshError exposes only stable, user-safe categories. Provider errors can
// contain response details, so they must never be serialized directly into usage JSON.
func usageRefreshError(err error) string {
	if err == nil {
		return ""
	}
	var rejected *upstream.UpstreamError
	switch {
	case errors.Is(err, accounts.ErrReauthorize):
		return "reauthorization_required"
	case errors.Is(err, accounts.ErrRefresh):
		return "credential_refresh_failed"
	case errors.Is(err, upstream.ErrUsageUnsupported):
		return "provider_usage_unsupported"
	case errors.Is(err, context.DeadlineExceeded):
		return "refresh_timeout"
	case errors.Is(err, upstream.ErrResponse):
		return "invalid_upstream_response"
	case errors.As(err, &rejected) && rejected.Status == 429:
		return "rate_limited"
	case errors.As(err, &rejected) && rejected.Status >= 400 && rejected.Status < 500:
		return "upstream_rejected"
	case errors.As(err, &rejected) && rejected.Status >= 500:
		return "upstream_unavailable"
	case errors.Is(err, upstream.ErrUpstream):
		return "upstream_unavailable"
	default:
		return "refresh_failed"
	}
}

func (s *Service) acquireUsage() (func(), error) {
	select {
	case s.usage.slots <- struct{}{}:
		release, err := s.Acquire()
		if err != nil {
			<-s.usage.slots
			return nil, err
		}
		return func() { release(); <-s.usage.slots }, nil
	default:
		return nil, ErrBusy
	}
}
