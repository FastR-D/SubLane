package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/murongg/SubLane/internal/storage/db"
)

// A cycle records the last observed balance, never an estimate of consumption at reset.
type SchedulingCycle struct {
	AccountID      string  `json:"account_id"`
	Lifecycle      int64   `json:"lifecycle"`
	Scope          string  `json:"scope"`
	ResetAt        int64   `json:"reset_at"`
	WindowSeconds  int64   `json:"window_seconds"`
	Remaining      float64 `json:"remaining"`
	ObservationAge int64   `json:"observation_age"`
	Observations   int64   `json:"observations"`
	Complete       bool    `json:"complete"`
	PolicyRevision int64   `json:"policy_revision"`
}

func (s *Service) recordSchedulingCycle(ctx context.Context, q *db.Queries, v SchedulingSample) error {
	if v.Remaining == nil || v.ResetAt == nil || v.WindowSeconds == nil || *v.WindowSeconds <= 0 || *v.ResetAt <= v.ObservedAt {
		return nil
	}
	raw, err := q.PreviousSchedulingQuota(ctx, db.PreviousSchedulingQuotaParams{TenantID: s.tenantID, AccountID: v.AccountID, Revision: v.Revision, ObservedAt: v.ObservedAt, Payload: v.Scope})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var old SchedulingSample
	if err = json.Unmarshal([]byte(raw), &old); err != nil {
		return err
	}
	if old.Remaining == nil || old.ResetAt == nil || old.WindowSeconds == nil || *old.WindowSeconds != *v.WindowSeconds || *old.ResetAt > v.ObservedAt || *old.ResetAt >= *v.ResetAt || *old.Remaining > *v.Remaining || old.ObservedAt > *old.ResetAt {
		return nil
	}
	coverage, err := q.SchedulingWindowCoverage(ctx, db.SchedulingWindowCoverageParams{TenantID: s.tenantID, AccountID: v.AccountID, Revision: v.Revision, Scope: old.storageScope()})
	if err != nil {
		return err
	}
	start := *old.ResetAt - *old.WindowSeconds
	policy, err := q.SchedulingWindowPolicy(ctx, db.SchedulingWindowPolicyParams{TenantID: s.tenantID, StartAt: start, ResetAt: *old.ResetAt})
	if err != nil {
		return err
	}
	// Require observations across most five-minute buckets, fresh observations on
	// both sides of reset, and one policy throughout the window. Sparse or rolling
	// windows remain useful evidence but cannot rank parameter revisions.
	tolerance := min(int64(300), max(int64(30), *old.WindowSeconds/10))
	if math.Abs(float64(*v.ResetAt-*old.ResetAt-*old.WindowSeconds)) > float64(tolerance) {
		return nil
	}
	complete := coverage.Observations >= max(int64(3), (*old.WindowSeconds/300)*8/10) && coverage.FirstAt <= start+tolerance && *old.ResetAt-old.ObservedAt <= tolerance && v.ObservedAt-*old.ResetAt <= tolerance && policy.Changes == 0
	var completeInt int64
	if complete {
		completeInt = 1
	}
	return q.InsertSchedulingCycle(ctx, db.InsertSchedulingCycleParams{TenantID: s.tenantID, AccountID: v.AccountID, Scope: v.Scope, Lifecycle: v.Revision, ResetAt: *old.ResetAt, WindowSeconds: *old.WindowSeconds, Remaining: *old.Remaining, ObservationAge: *old.ResetAt - old.ObservedAt, Observations: coverage.Observations, Complete: completeInt, PolicyRevision: policy.Revision})
}

// Evaluations compare observed outcomes only. They do not simulate unavailable
// provider capacity or automatically apply parameters.
type SchedulingEvaluation struct {
	CompleteCycles      int                          `json:"complete_cycles"`
	ObservedCycles      int                          `json:"observed_cycles"`
	Cohorts             int                          `json:"cohorts"`
	Reports             []SchedulingEvaluationReport `json:"reports"`
	RecommendedRevision *int64                       `json:"recommended_revision"`
}
type SchedulingEvaluationReport struct {
	Revision      int64   `json:"revision"`
	Cycles        int     `json:"cycles"`
	MeanRemaining float64 `json:"mean_remaining"`
	Comparable    bool    `json:"comparable"`
}

func evaluateScheduling(h SchedulingHistory) SchedulingEvaluation {
	result := SchedulingEvaluation{ObservedCycles: len(h.Cycles), Reports: []SchedulingEvaluationReport{}}
	type balance struct {
		sum   float64
		count int
	}
	byRevision := map[int64]map[string]balance{}
	cohorts := map[string]bool{}
	for _, v := range h.Cycles {
		if !v.Complete {
			continue
		}
		result.CompleteCycles++
		key := fmt.Sprintf("%s:%d:%s:%d", v.AccountID, v.Lifecycle, v.Scope, v.WindowSeconds)
		cohorts[key] = true
		if byRevision[v.PolicyRevision] == nil {
			byRevision[v.PolicyRevision] = map[string]balance{}
		}
		b := byRevision[v.PolicyRevision][key]
		b.sum += v.Remaining
		b.count++
		byRevision[v.PolicyRevision][key] = b
	}
	result.Cohorts = len(cohorts)
	for revision, groups := range byRevision {
		report := SchedulingEvaluationReport{Revision: revision, Comparable: len(groups) == len(cohorts)}
		for _, b := range groups {
			report.Cycles += b.count
			report.MeanRemaining += b.sum / float64(b.count)
			if b.count < 3 {
				report.Comparable = false
			}
		}
		report.MeanRemaining /= float64(len(groups))
		result.Reports = append(result.Reports, report)
	}
	sort.Slice(result.Reports, func(i, j int) bool { return result.Reports[i].Revision < result.Reports[j].Revision })
	var requests, limited, errorsCount, wait, selected int64
	for _, v := range h.Usage {
		requests += v.Requests
		limited += v.Limited
		errorsCount += v.Errors
		wait += v.WaitMS
	}
	for _, v := range h.Samples {
		if v.Kind == "placement" {
			selected += v.Selected
		}
	}
	if h.Truncated || h.Dropped > 0 || requests < 100 || selected < 30 || limited > 0 || float64(errorsCount)/float64(requests) > .01 || wait/requests > 1000 {
		return result
	}
	// Equal cohort coverage avoids preferring a revision simply because it served
	// different accounts/windows. At least two independently observed revisions
	// and three complete cycles per cohort are required for a review suggestion.
	best := math.Inf(1)
	comparable := 0
	for _, v := range result.Reports {
		if !v.Comparable {
			continue
		}
		comparable++
		if v.MeanRemaining < best {
			best = v.MeanRemaining
			revision := v.Revision
			result.RecommendedRevision = &revision
		}
	}
	if comparable < 2 {
		result.RecommendedRevision = nil
	}
	return result
}
