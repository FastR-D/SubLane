package gateway

import (
	"context"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
	"math"
	"testing"
)

func TestSmoothSchedulerSpreadsWeightsAndIsolatesGroups(t *testing.T) {
	var scheduler smoothScheduler
	candidates := []weightedCandidate{{id: "A", weight: 90}, {id: "B", weight: 10}}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		got := scheduler.choose("group-1:model", candidates)
		counts[got]++
		if i == 19 && counts["B"] < 1 {
			t.Fatal("weighted rotation concentrated the first twenty placements", counts)
		}
	}
	if counts["A"] != 90 || counts["B"] != 10 {
		t.Fatal(counts)
	}
	for i := 0; i < 7; i++ {
		scheduler.choose("group-1:model", candidates)
	}
	var independent smoothScheduler
	for i := 0; i < 20; i++ {
		if a, b := scheduler.choose("group-2:model", candidates), independent.choose("group-2:model", candidates); a != b {
			t.Fatal("group state leaked", a, b)
		}
	}
}

func TestSmoothSchedulerChurnAndBoundedState(t *testing.T) {
	var scheduler smoothScheduler
	for i := 0; i < 2000; i++ {
		key := string(rune(i + 1))
		scheduler.choose(key, []weightedCandidate{{id: "A", weight: 1}, {id: "B", weight: 1}})
	}
	if len(scheduler.states) > maxSchedulingScopes {
		t.Fatal("unbounded scopes", len(scheduler.states))
	}
	candidates := []weightedCandidate{{id: "A", weight: 1}, {id: "B", weight: 1}}
	for i := 0; i < 100; i++ {
		scheduler.choose("churn", candidates)
		scheduler.choose("churn", []weightedCandidate{{id: "A", weight: 1}})
	}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		counts[scheduler.choose("churn", candidates)]++
	}
	if math.Abs(float64(counts["A"]-counts["B"])) > 2 {
		t.Fatal("returning account caught up or starved", counts)
	}
	for _, state := range scheduler.states {
		for _, credit := range state.credits {
			if math.IsNaN(credit) || math.Abs(credit) > 2 {
				t.Fatal("unbounded credits", credit)
			}
		}
	}
}

func TestSchedulingFactorsDegradePerAccount(t *testing.T) {
	tests := []struct {
		quota                  quotaDecision
		boost, inFlight, limit int64
		want                   float64
	}{
		{quotaDecision{State: "available", Weight: 90}, 0, 0, 10, .9},
		{quotaDecision{State: "available", Weight: 1}, 0, 0, 10, .05},
		{quotaDecision{State: "stale"}, 0, 0, 10, .5},
		{quotaDecision{State: "unknown"}, 0, 0, 10, .5},
		{quotaDecision{State: "unsupported"}, 0, 0, 10, 1},
		{quotaDecision{State: "available", Weight: 80}, 50, 5, 10, .8},
	}
	for _, tc := range tests {
		v := schedulingFactors(tc.quota, tc.boost, tc.inFlight, tc.limit, true)
		if math.Abs(v.EffectiveWeight-tc.want) > 1e-9 {
			t.Fatal(tc, v)
		}
	}
}

func TestGroupSchedulingPreviewUsesSharedLoadAndLocalBoostsWithoutPlacement(t *testing.T) {
	s, first, second := reviewGateway(t)
	ctx := context.Background()
	s.policy.QuotaWeighted = true
	service := groups.New(s.db)
	group, err := service.Save(ctx, 0, groups.Input{Name: "Shared", Enabled: true, AccountIDs: []string{first, second}, PreferenceBoosts: map[string]int64{first: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.loadRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	s.health[first].InFlight = s.health[first].MaxConcurrency / 2
	for _, id := range []string{first, second} {
		saveProviderQuota(t, s, id, upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{UsedPercent: quotaFloat(20)}}}}})
	}
	var initialBindings int
	if err := s.db.QueryRow("SELECT count(*) FROM account_affinity").Scan(&initialBindings); err != nil {
		t.Fatal(err)
	}
	previews := map[int64]SchedulingPreview{}
	for _, groupID := range []int64{1, group.ID} {
		before := s.scheduler.clock
		value, err := s.Scheduling(ctx, 1, groupID, "synthetic-model")
		if err != nil || len(value.Weights) != 2 {
			t.Fatal(value, err)
		}
		if before != s.scheduler.clock {
			t.Fatal("preview advanced placement")
		}
		previews[groupID] = value
	}
	lookup := func(g int64, id string) SchedulingWeight {
		for _, w := range previews[g].Weights {
			if w.AccountID == id {
				return w
			}
		}
		t.Fatal("missing candidate")
		return SchedulingWeight{}
	}
	if lookup(1, first).LoadFactor != lookup(group.ID, first).LoadFactor {
		t.Fatal("group duplicated account load")
	}
	if lookup(group.ID, first).EffectiveWeight != 2*lookup(1, first).EffectiveWeight {
		t.Fatal("group boost not applied independently")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM account_affinity").Scan(&count); err != nil || count != initialBindings {
		t.Fatal("preview created affinity", count, err)
	}
	bound, _, err := s.selectAccount(ctx, 1, group.ID, "synthetic-sticky", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, group.ID, groups.Input{Name: "Shared", Enabled: true, AccountIDs: []string{first, second}, PreferenceBoosts: map[string]int64{first: 0, second: 300}}); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		got, _, err := s.selectAccount(ctx, 1, group.ID, "synthetic-sticky", "", "synthetic-model", Responses)
		if err != nil || got != bound {
			t.Fatal("boost moved binding", got, bound, err)
		}
	}
	s.health[first].InFlight = s.health[first].MaxConcurrency
	preview, err := s.Scheduling(ctx, 1, group.ID, "synthetic-model")
	if err != nil || len(preview.Weights) != 1 || preview.Weights[0].AccountID != second {
		t.Fatal("shared saturation not filtered", preview, err)
	}
}
