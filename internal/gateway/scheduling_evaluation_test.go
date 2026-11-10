package gateway

import "testing"

func TestSchedulingEvaluationRequiresMatchedCohortsAndDemand(t *testing.T) {
	h := SchedulingHistory{Usage: []SchedulingUsage{{Requests: 100}}, Samples: []SchedulingSample{{Kind: "placement", Selected: 30}}}
	for revision := int64(1); revision <= 2; revision++ {
		for range 3 {
			h.Cycles = append(h.Cycles, SchedulingCycle{AccountID: "synthetic", Scope: "primary", WindowSeconds: 3600, Complete: true, PolicyRevision: revision, Remaining: float64(30 - revision*10)})
		}
	}
	e := evaluateScheduling(h)
	if e.RecommendedRevision == nil || *e.RecommendedRevision != 2 || e.CompleteCycles != 6 {
		t.Fatal(e)
	}
	for _, mutate := range []func(*SchedulingHistory){
		func(h *SchedulingHistory) { h.Truncated = true },
		func(h *SchedulingHistory) { h.Dropped = 1 },
		func(h *SchedulingHistory) { h.Usage = []SchedulingUsage{{Requests: 99}} },
		func(h *SchedulingHistory) { h.Usage = []SchedulingUsage{{Requests: 100, Limited: 1}} },
		func(h *SchedulingHistory) { h.Samples = nil },
		func(h *SchedulingHistory) {
			h.Cycles = append(append([]SchedulingCycle{}, h.Cycles...), SchedulingCycle{AccountID: "different", Scope: "primary", Complete: true, PolicyRevision: 1})
		},
		func(h *SchedulingHistory) {
			h.Cycles = append([]SchedulingCycle{}, h.Cycles...)
			h.Cycles[0].Complete = false
		},
	} {
		copy := h
		mutate(&copy)
		if got := evaluateScheduling(copy); got.RecommendedRevision != nil {
			t.Fatal("insufficient evidence recommended a policy", got)
		}
	}
}
