package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/upstream"
)

func TestSchedulingHistoryPersistsObservationsAndPreviewIsPure(t *testing.T) {
	s, first, _ := reviewGateway(t)
	s.scheduling = schedulingCollector{}
	ctx := context.Background()
	if err := s.loadRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	s.recordSchedulingQuota(first, 1, upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(30), WindowSeconds: quotaInt(14400), ResetAt: quotaInt(s.now().Unix() + 1800)}}}}})
	for range 3 {
		if _, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.collectScheduling(ctx); err != nil {
		t.Fatal(err)
	}
	h, err := s.SchedulingHistory(ctx, 1, 1, 24, "")
	if err != nil || len(h.Samples) == 0 || h.CollectedSince == 0 {
		t.Fatal(h, err)
	}
	var expected, selected float64
	quota := false
	for _, v := range h.Samples {
		if v.Kind == "placement" {
			expected += v.Expected
			selected += float64(v.Selected)
		}
		if v.Kind == "quota" {
			quota = true
		}
	}
	if expected < 2.99 || selected != 3 || !quota {
		t.Fatal(expected, selected, quota)
	}
	before := s.scheduling.pendingCount()
	if _, err := s.Scheduling(ctx, 1, 1, "synthetic-model"); err != nil {
		t.Fatal(err)
	}
	if s.scheduling.pendingCount() != before {
		t.Fatal("preview collected placement")
	}
	loaded := New(ctx, s.db, s.accounts, s.provider)
	defer loaded.Close()
	again, err := loaded.SchedulingHistory(ctx, 1, 1, 24, "")
	if err != nil || len(again.Samples) != len(h.Samples) {
		t.Fatal(again, err)
	}
	if _, err := loaded.SchedulingHistory(ctx, 999, 1, 24, ""); err == nil {
		t.Fatal("unauthorized group history")
	}
	if _, err := loaded.SchedulingHistory(ctx, 1, 1, 2161, ""); err == nil {
		t.Fatal("unbounded range")
	}
}

func TestSchedulingCyclesRequireObservedResetRatherThanRollingEstimate(t *testing.T) {
	s, id, _ := reviewGateway(t)
	s.scheduling = schedulingCollector{}
	now := time.Now().Unix()
	s.now = func() time.Time { return time.Unix(now, 0) }
	ctx := context.Background()
	observe := func(used float64, reset int64) {
		s.recordSchedulingQuota(id, 0, upstream.Usage{UpdatedAt: now, Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(used), WindowSeconds: quotaInt(3600), ResetAt: quotaInt(reset)}}}}})
		if err := s.flushScheduling(ctx); err != nil {
			t.Fatal(err)
		}
	}
	reset := now + 60
	observe(50, reset)
	now += 10
	observe(45, reset+30)
	h, err := s.SchedulingHistory(ctx, 1, 1, 24, id)
	if err != nil || len(h.Cycles) != 0 {
		t.Fatal(h.Cycles, err)
	}
	now = reset + 31
	observe(5, reset+3630)
	h, err = s.SchedulingHistory(ctx, 1, 1, 24, id)
	if err != nil || len(h.Cycles) != 1 || h.Cycles[0].Remaining != 55 || h.Cycles[0].Complete {
		t.Fatal(h.Cycles, err)
	}
}

func TestSchedulingAccountUsageSettlesOnce(t *testing.T) {
	s, _, _ := reviewGateway(t)
	ctx := context.Background()
	x, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic request"}`), nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	if err = x.Events(func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = x.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err = x.Body.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := s.SchedulingHistory(ctx, 1, 1, 24, "")
	if err != nil || len(h.Usage) != 1 || h.Usage[0].Requests != 1 || h.Usage[0].Completed != 1 {
		t.Fatal(h.Usage, err)
	}
}

func TestSchedulingCollectorIsBoundedAndKeepsHiddenQuotaPrivate(t *testing.T) {
	s, id, _ := reviewGateway(t)
	s.scheduling = schedulingCollector{}
	s.recordSchedulingQuota(id, 0, upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Hidden: true, Name: "internal", Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(40)}}}}})
	if s.scheduling.pendingCount() != 0 {
		t.Fatal("hidden history exposed")
	}
	for i := range 5000 {
		s.scheduling.add(SchedulingSample{Kind: "pressure", AccountID: id, Bucket: int64(i), Samples: 1})
	}
	if s.scheduling.pendingCount() > maxSchedulingPending || s.scheduling.dropped == 0 {
		t.Fatal("unbounded collector")
	}
}

func TestSchedulingCyclePersistsCompleteObservedWindow(t *testing.T) {
	s, id, _ := reviewGateway(t)
	s.scheduling = schedulingCollector{}
	now := time.Now().Unix() / 300 * 300
	s.now = func() time.Time { return time.Unix(now, 0) }
	ctx := context.Background()
	start := now
	reset := start + 3600
	observe := func(used float64, next int64) {
		s.recordSchedulingQuota(id, 0, upstream.Usage{UpdatedAt: now, Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(used), WindowSeconds: quotaInt(3600), ResetAt: quotaInt(next)}}}}})
		if err := s.flushScheduling(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for i := int64(0); i < 12; i++ {
		now = start + i*300
		observe(float64(i)*5, reset)
	}
	now = reset - 1
	observe(70, reset)
	now = reset + 1
	observe(1, reset+3600)
	h, err := s.SchedulingHistory(ctx, 1, 1, 24, id)
	if err != nil || len(h.Cycles) != 1 || !h.Cycles[0].Complete || h.Cycles[0].Remaining != 30 || h.Evaluation.CompleteCycles != 1 {
		t.Fatal(h.Cycles, h.Evaluation, err)
	}
	observe(1, reset+3600)
	h, err = s.SchedulingHistory(ctx, 1, 1, 24, id)
	if err != nil || len(h.Cycles) != 1 {
		t.Fatal("reset recorded twice", h.Cycles, err)
	}
}
