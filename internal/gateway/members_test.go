package gateway

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/tenants"
)

func TestMemberRemovalAllowsInFlightHistoryButRejectsNewRequests(t *testing.T) {
	ctx := context.Background()
	s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	member := runtimeMember(t, s)
	raw := []byte(`{"model":"synthetic-model","input":"synthetic"}`)
	response, err := s.Open(ctx, member, 1, raw, nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenants.New(s.db).RemoveMember(ctx, 1, 1, member); err != nil {
		t.Fatal(err)
	}
	if err := response.Events(func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	history, err := s.Requests(ctx, RequestFilter{MemberID: member})
	if err != nil || len(history.Requests) != 1 || history.Requests[0].Outcome != "success" {
		t.Fatalf("in-flight history lost: %+v %v", history, err)
	}
	statistics, err := s.Statistics(ctx, 1)
	if err != nil || statistics.Totals.Completed != 1 {
		t.Fatalf("in-flight usage lost: %+v %v", statistics, err)
	}
	if response, err := s.Open(ctx, member, 1, raw, nil, Responses); err == nil {
		response.Body.Close()
		t.Fatal("removed member opened another request")
	}
}

func runtimeMember(t *testing.T, s *Service) int64 {
	t.Helper()
	a, err := auth.New(s.db)
	if err != nil {
		t.Fatal(err)
	}
	m, err := a.CreateMember(context.Background(), "limited-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := groups.New(s.db).SetMemberGroups(context.Background(), m.ID, []int64{1}); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func TestMemberLimitsShareKeysGroupsAndSurviveRestart(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int64
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return syntheticStream(), nil }))
	member := runtimeMember(t, s)
	initial, err := s.MemberLimits(ctx, member)
	if err != nil || initial.RequestsPerMinute != 0 || initial.MaxConcurrency != 10 {
		t.Fatal("limits must default to unlimited RPM and 10 concurrent requests", initial, err)
	}
	if err := s.SetMemberLimits(ctx, member, 2, 1); err != nil {
		t.Fatal(err)
	}
	pool, err := groups.New(s.db).Save(ctx, 0, groups.Input{Name: "Second pool", Enabled: true, AccountIDs: []string{ids["codex"]}})
	if err != nil {
		t.Fatal(err)
	}
	if err := groups.New(s.db).SetMemberGroups(ctx, member, []int64{1, pool.ID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Minute).Add(10 * time.Second)
	s.now = func() time.Time { return now }
	raw := []byte(`{"model":"synthetic-model","input":"synthetic"}`)
	createAdmissionKey(t, s, 11, member, 1)
	createAdmissionKey(t, s, 12, member, pool.ID)
	first, err := s.Open(WithRequestIdentity(ctx, 11, "http"), member, 1, raw, nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(WithRequestIdentity(ctx, 12, "websocket"), member, pool.ID, raw, nil, Responses); !errors.Is(err, ErrMemberBusy) {
		t.Fatal("other key/group bypassed concurrency", err)
	}
	first.Body.Close()
	first.Body.Close()
	second, err := s.Open(WithRequestIdentity(ctx, 12, "websocket"), member, pool.ID, raw, nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	restarted := New(ctx, s.db, s.accounts, s.provider)
	defer restarted.Close()
	restarted.now = s.now
	if _, err := restarted.Open(ctx, member, 1, raw, nil, Responses); !errors.Is(err, ErrMemberRate) {
		t.Fatal("restart lost minute counter", err)
	}
	if calls.Load() != 2 {
		t.Fatal("rejected requests reached upstream", calls.Load())
	}
	now = now.Add(time.Minute)
	next, err := restarted.Open(ctx, member, 1, raw, nil, Responses)
	if err != nil {
		t.Fatal("next window remained blocked", err)
	}
	next.Body.Close()
	if err := s.SetMemberLimits(ctx, 1, 1, 1); err != nil {
		t.Fatal("owner policy could not be changed", err)
	}
	for _, v := range [][2]int64{{-1, 1}, {6001, 1}, {1, -1}, {1, 1001}} {
		if err := s.SetMemberLimits(ctx, member, v[0], v[1]); err == nil {
			t.Fatal("invalid limits accepted", v)
		}
	}
	for _, concurrency := range []int64{0, 10, 64, 1000} {
		if err := s.SetMemberLimits(ctx, member, 0, concurrency); err != nil {
			t.Fatalf("set concurrency %d: %v", concurrency, err)
		}
		policy, err := s.MemberLimits(ctx, member)
		if err != nil || policy.MaxConcurrency != concurrency {
			t.Fatalf("concurrency %d was not persisted: %+v %v", concurrency, policy, err)
		}
	}
}

func TestDefaultMemberConcurrencyLimitsRequests(t *testing.T) {
	ctx := context.Background()
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	member := runtimeMember(t, s)
	if err := s.SetConcurrency(ctx, ids["codex"], 8); err != nil {
		t.Fatal(err)
	}
	if _, err := s.accounts.Authorize(ctx, "Second", accounts.Credential{AccountID: "synthetic-second", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, ""); err != nil {
		t.Fatal(err)
	}
	configureTestPool(t, s.db)
	raw := []byte(`{"model":"synthetic-model","input":"synthetic"}`)
	active := make([]*Exchange, 0, 11)
	t.Cleanup(func() {
		for _, exchange := range active {
			exchange.Body.Close()
		}
	})
	for range 10 {
		exchange, err := s.Open(ctx, member, 1, raw, nil, Responses)
		if err != nil {
			t.Fatal("default limit rejected one of the first 10 requests", err)
		}
		active = append(active, exchange)
	}
	if _, err := s.Open(ctx, member, 1, raw, nil, Responses); !errors.Is(err, ErrMemberBusy) {
		t.Fatal("default limit did not reject the 11th request", err)
	}
	active[0].Body.Close()
	exchange, err := s.Open(ctx, member, 1, raw, nil, Responses)
	if err != nil {
		t.Fatal("closing a request did not release the member slot", err)
	}
	active = append(active, exchange)
}

func TestMemberConcurrencyIsAtomicAndCancellationReleases(t *testing.T) {
	ctx := context.Background()
	s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	member := runtimeMember(t, s)
	if err := s.SetMemberLimits(ctx, member, 0, 1); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"model":"synthetic-model","input":"synthetic"}`)
	started := make(chan struct{})
	results := make(chan *Exchange, 16)
	var wg sync.WaitGroup
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for range 16 {
		wg.Go(func() {
			<-started
			r, err := s.Open(requestCtx, member, 1, raw, nil, Responses)
			if err != nil && !errors.Is(err, ErrMemberBusy) {
				t.Error(err)
			}
			if r != nil {
				results <- r
			}
		})
	}
	close(started)
	wg.Wait()
	close(results)
	if len(results) != 1 {
		t.Fatal("concurrent policy was exceeded", len(results))
	}
	cancel()
	for r := range results {
		r.Body.Close()
	}
	policy, err := s.MemberLimits(ctx, member)
	if err != nil || policy.InFlight != 0 {
		t.Fatal("member lease leaked", policy, err)
	}
}
