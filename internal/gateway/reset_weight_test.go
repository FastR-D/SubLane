package gateway

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestResetWeightUsesGoverningWindowAndFreshTiming(t *testing.T) {
	now := time.Unix(1800000000, 0)
	policy := groups.DefaultSchedulingPolicy()
	policy.Enabled = true
	window := func(used float64, seconds, reset int64) upstream.UsageWindow {
		return upstream.UsageWindow{Kind: "primary", UsedPercent: &used, WindowSeconds: &seconds, ResetAt: &reset}
	}
	for _, tc := range []struct {
		name    string
		age     int64
		windows []upstream.UsageWindow
		want    float64
	}{
		{"near", 0, []upstream.UsageWindow{window(40, 14400, now.Unix()+1800)}, 1.25},
		{"outside horizon", 0, []upstream.UsageWindow{window(40, 14400, now.Unix()+7200)}, 1},
		{"long constraint wins", 0, []upstream.UsageWindow{window(20, 14400, now.Unix()+60), window(95, 604800, now.Unix()+259200)}, 1},
		{"ties conservative", 0, []upstream.UsageWindow{window(40, 14400, now.Unix()+60), window(40, 604800, now.Unix()+259200)}, 1},
		{"stale", 121, []upstream.UsageWindow{window(40, 14400, now.Unix()+60)}, 1},
		{"passed", 0, []upstream.UsageWindow{window(40, 14400, now.Unix())}, 1},
		{"exhausted", 0, []upstream.UsageWindow{window(100, 14400, now.Unix()+60)}, 1},
		{"duration unknown", 0, []upstream.UsageWindow{{UsedPercent: quotaFloat(40), ResetAt: quotaInt(now.Unix() + 60)}}, 1},
		{"inconsistent", 0, []upstream.UsageWindow{window(40, 100, now.Unix()+101)}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := upstream.Usage{UpdatedAt: now.Unix() - tc.age, Limits: []upstream.UsageLimit{{Windows: tc.windows}}}
			got := resetWeight(value, now, policy)
			if math.Abs(got.Factor-tc.want) > 1e-9 {
				t.Fatalf("got %+v, want %v", got, tc.want)
			}
		})
	}
}

func TestResetPolicyVersionsPersistAndRejectConflicts(t *testing.T) {
	s, _, _ := reviewGateway(t)
	ctx := context.Background()
	service := groups.New(s.db)
	p, err := service.SchedulingPolicy(ctx)
	if err != nil || p.Revision != 0 || p.Enabled {
		t.Fatal(p, err)
	}
	p.Enabled = true
	saved, err := service.SaveSchedulingPolicy(ctx, p)
	if err != nil || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	if _, err = service.SaveSchedulingPolicy(ctx, p); !errors.Is(err, groups.ErrSchedulingConflict) {
		t.Fatal("stale save", err)
	}
	p.MaxMultiplier = math.NaN()
	if _, err = service.SaveSchedulingPolicy(ctx, p); !errors.Is(err, groups.ErrInput) {
		t.Fatal("nonfinite", err)
	}
	loaded, err := groups.New(s.db).SchedulingPolicy(ctx)
	if err != nil || loaded != saved {
		t.Fatal(loaded, err)
	}
	versions, err := service.SchedulingRevisions(ctx)
	if err != nil || len(versions) != 1 || versions[0].Revision != 1 {
		t.Fatal(versions, err)
	}
}

func TestResetPlacementPrefersNearWindowWithoutMovingBindings(t *testing.T) {
	s, near, far := reviewGateway(t)
	ctx := context.Background()
	s.policy.QuotaWeighted = true
	p := groups.DefaultSchedulingPolicy()
	p.Enabled = true
	if _, err := groups.New(s.db).SaveSchedulingPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	for id, seconds := range map[string]int64{near: 1800, far: 7200} {
		saveProviderQuota(t, s, id, upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(40), WindowSeconds: quotaInt(14400), ResetAt: quotaInt(s.now().Unix() + seconds)}}}}})
	}
	counts := map[string]int{}
	for range 225 {
		id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses)
		if err != nil {
			t.Fatal(err)
		}
		counts[id]++
	}
	if counts[near] != 125 || counts[far] != 100 {
		t.Fatal(counts)
	}
	preview, err := s.Scheduling(ctx, 1, 1, "synthetic-model")
	if err != nil || preview.Policy.Revision != 1 {
		t.Fatal(preview, err)
	}
	bound, _, err := s.selectAccount(ctx, 1, 1, "reset-bound", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		id, _, err := s.selectAccount(ctx, 1, 1, "reset-bound", "", "synthetic-model", Responses)
		if err != nil || id != bound {
			t.Fatal(id, err)
		}
	}
	s.policy.QuotaWeighted = false
	preview, err = s.Scheduling(ctx, 1, 1, "synthetic-model")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range preview.Weights {
		if v.Reset.Factor != 1 || v.QuotaFactor != 1 || v.LoadFactor != 1 {
			t.Fatal(v)
		}
	}
}

func TestResetDraftUsesSameSnapshotAndRejectsStaleRevision(t *testing.T) {
	s, near, _ := reviewGateway(t)
	ctx := context.Background()
	s.policy.QuotaWeighted = true
	now := s.now().Unix()
	saveProviderQuota(t, s, near, upstream.Usage{UpdatedAt: now, Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(40), WindowSeconds: quotaInt(14400), ResetAt: quotaInt(now + 1800)}}}}})
	p := groups.DefaultSchedulingPolicy()
	p.Enabled = true
	before := s.scheduler.clock
	draft, err := s.PreviewSchedulingPolicy(ctx, 1, 1, "synthetic-model", p)
	if err != nil || len(draft.Baseline) != 2 || s.scheduler.clock != before {
		t.Fatal(draft, err)
	}
	for i, w := range draft.Weights {
		if w.AccountID == near && w.EffectiveWeight != draft.Baseline[i].EffectiveWeight*1.25 {
			t.Fatal("draft and baseline snapshots differ", draft)
		}
	}
	if _, err := groups.New(s.db).SaveSchedulingPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewSchedulingPolicy(ctx, 1, 1, "synthetic-model", p); !errors.Is(err, groups.ErrSchedulingConflict) {
		t.Fatal("stale preview", err)
	}
}
