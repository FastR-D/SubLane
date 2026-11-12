package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/upstream"
)

func TestIPReportCacheNeverDisclosesOnReadAndPacesLookups(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}
	var calls atomic.Int32
	release := make(chan struct{})
	fail := atomic.Bool{}
	cache := &ipReportCache{entries: map[string]*ipReportEntry{}, now: clock, lookup: func(_ context.Context, ip string) (upstream.IPReport, error) {
		calls.Add(1)
		<-release
		if fail.Load() {
			return upstream.IPReport{}, &upstream.UpstreamError{Status: 429}
		}
		return upstream.IPReport{IP: ip}, nil
	}}
	ctx := context.Background()
	if _, err := cache.cached("203.0.113.9"); !errors.Is(err, errIPReportMissing) || calls.Load() != 0 {
		t.Fatal("a cache read contacted the service", err, calls.Load())
	}
	// Concurrent explicit lookups for one IP share a single upstream request.
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if report, _, err := cache.fetch(ctx, "203.0.113.9"); err != nil || report.IP != "203.0.113.9" {
				t.Error(report, err)
			}
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	group.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent lookups were not coalesced", calls.Load())
	}
	if report, err := cache.cached("203.0.113.9"); err != nil || report.IP != "203.0.113.9" {
		t.Fatal("successful lookup was not cached", err)
	}
	if _, wait, err := cache.fetch(ctx, "203.0.113.9"); err != nil || wait <= 0 || calls.Load() != 1 {
		t.Fatal("a refresh inside the cooldown reached the service", wait, err, calls.Load())
	}
	// A failed attempt is paced from when it started, and 429 backs off longer.
	fail.Store(true)
	if _, _, err := cache.fetch(ctx, "198.51.100.7"); err == nil {
		t.Fatal("upstream failure was hidden")
	}
	if _, wait, err := cache.fetch(ctx, "198.51.100.7"); !errors.Is(err, errIPReportPaced) || wait < ipReportCooldown || calls.Load() != 2 {
		t.Fatal("failed lookup was retried immediately", wait, err, calls.Load())
	}
	advance(ipReportCooldown)
	if _, _, err := cache.fetch(ctx, "198.51.100.7"); !errors.Is(err, errIPReportPaced) || calls.Load() != 2 {
		t.Fatal("rate-limited lookup did not back off", err, calls.Load())
	}
	fail.Store(false)
	advance(ipReportBackoff)
	if _, _, err := cache.fetch(ctx, "198.51.100.7"); err != nil || calls.Load() != 3 {
		t.Fatal("lookup did not resume after backoff", err, calls.Load())
	}
	advance(ipReportTTL)
	if _, err := cache.cached("203.0.113.9"); !errors.Is(err, errIPReportMissing) {
		t.Fatal("expired report was served", err)
	}
	for i := range ipReportEntries + 5 {
		advance(ipReportCooldown)
		if _, _, err := cache.fetch(ctx, fmt.Sprintf("192.0.2.%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) > ipReportEntries {
		t.Fatal("cache is unbounded", len(cache.entries))
	}
}

func TestIPReportCacheRejectsNewIPsWhenEverySlotIsInFlight(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	cache := &ipReportCache{entries: map[string]*ipReportEntry{}, now: time.Now, lookup: func(_ context.Context, ip string) (upstream.IPReport, error) {
		calls.Add(1)
		<-release
		return upstream.IPReport{IP: ip}, nil
	}}
	var group sync.WaitGroup
	for i := range ipReportEntries {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _, _ = cache.fetch(context.Background(), fmt.Sprintf("192.0.2.%d", i))
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for int(calls.Load()) < ipReportEntries && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, _, err := cache.fetch(context.Background(), "198.51.100.200"); !errors.Is(err, errIPReportPaced) {
		t.Fatal("a new IP was admitted while every slot was in flight", err)
	}
	cache.mu.Lock()
	size := len(cache.entries)
	cache.mu.Unlock()
	close(release)
	group.Wait()
	if size > ipReportEntries || int(calls.Load()) != ipReportEntries {
		t.Fatal("cache exceeded its bound", size, calls.Load())
	}
}
