package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

const maxSchedulingScopes = 512

type weightedCandidate struct {
	id     string
	weight float64
	epoch  string
}
type smoothState struct {
	credits map[string]float64
	epochs  map[string]string
	touched uint64
	tie     uint64
}
type smoothScheduler struct {
	states map[string]*smoothState
	clock  uint64
}

// Caller holds the gateway admission lock. Credits are normalized selection shares,
// so changed quota/load weights cannot retain large historical ticket balances.
func (s *smoothScheduler) choose(scope string, candidates []weightedCandidate) string {
	if s.states == nil {
		s.states = make(map[string]*smoothState)
	}
	s.clock++
	state := s.states[scope]
	if state == nil {
		if len(s.states) >= maxSchedulingScopes {
			oldest := ""
			age := uint64(math.MaxUint64)
			for key, value := range s.states {
				if value.touched < age {
					oldest, age = key, value.touched
				}
			}
			delete(s.states, oldest)
		}
		state = &smoothState{credits: map[string]float64{}, epochs: map[string]string{}}
		s.states[scope] = state
	}
	state.touched = s.clock
	present := make(map[string]bool, len(candidates))
	total := 0.0
	for _, c := range candidates {
		present[c.id] = true
		total += c.weight
	}
	for id := range state.credits {
		if !present[id] {
			delete(state.credits, id)
			delete(state.epochs, id)
		}
	}
	for _, c := range candidates {
		if state.epochs[c.id] != c.epoch {
			state.credits[c.id] = 0
			state.epochs[c.id] = c.epoch
		}
		state.credits[c.id] = max(-1, min(1, state.credits[c.id]+c.weight/total))
	}
	// Rotate the tie origin even when lifecycle changes reset all current credits.
	best := int(state.tie % uint64(len(candidates)))
	for step := 1; step < len(candidates); step++ {
		i := (int(state.tie%uint64(len(candidates))) + step) % len(candidates)
		if state.credits[candidates[i].id] > state.credits[candidates[best].id]+1e-12 {
			best = i
		}
	}
	state.tie++
	chosen := candidates[best].id
	state.credits[chosen]--
	return chosen
}

type SchedulingWeight struct {
	AccountID       string  `json:"account_id"`
	QuotaState      string  `json:"quota_state"`
	QuotaUpdatedAt  *int64  `json:"quota_updated_at"`
	QuotaFactor     float64 `json:"quota_factor"`
	LoadFactor      float64 `json:"load_factor"`
	PreferenceBoost int64   `json:"preference_boost"`
	EffectiveWeight float64 `json:"effective_weight"`
	TargetShare     float64 `json:"target_share"`
	InFlight        int64   `json:"in_flight"`
	MaxConcurrency  int64   `json:"max_concurrency"`
}

func schedulingFactors(quota quotaDecision, boost, inFlight, limit int64, dynamic bool) SchedulingWeight {
	q, l := 1.0, 1.0
	if dynamic {
		switch {
		case quota.Weight > 0:
			q = max(.05, float64(quota.Weight)/100)
		case quota.State == "unsupported":
			q = 1
		default:
			q = .5
		}
		l = 1 / (1 + float64(inFlight)/float64(max(1, limit)))
	}
	return SchedulingWeight{QuotaState: quota.State, QuotaFactor: q, LoadFactor: l, PreferenceBoost: boost, EffectiveWeight: q * l * (1 + float64(boost)/100), InFlight: inFlight, MaxConcurrency: limit}
}

func (s *Service) candidateWeights(candidates []string, snapshots map[string]db.ListRoutingAccountsRow, model string) ([]weightedCandidate, []SchedulingWeight) {
	weighted := make([]weightedCandidate, 0, len(candidates))
	details := make([]SchedulingWeight, 0, len(candidates))
	total := 0.0
	for _, id := range candidates {
		row := snapshots[id]
		state := s.health[id]
		q := s.snapshotQuota(row.Provider, model, row.UsageSnapshot, row.UsageUpdatedAt)
		v := schedulingFactors(q, row.PreferenceBoost, state.InFlight, state.MaxConcurrency, s.policy.QuotaWeighted)
		v.AccountID = id
		v.QuotaUpdatedAt = row.UsageUpdatedAt
		// Authorization/runtime epochs and explicit group preferences invalidate old
		// credit; changing observations and load retain bounded smooth progress.
		epoch := fmt.Sprintf("%d:%d:%d", row.ModelsRevision, state.revision, row.PreferenceBoost)
		weighted = append(weighted, weightedCandidate{id: id, weight: v.EffectiveWeight, epoch: epoch})
		details = append(details, v)
		total += v.EffectiveWeight
	}
	for i := range details {
		details[i].TargetShare = details[i].EffectiveWeight / total
	}
	return weighted, details
}

type SchedulingPreview struct {
	Dynamic bool               `json:"dynamic"`
	Weights []SchedulingWeight `json:"weights"`
	Route   *RouteExplanation  `json:"route"`
}
type schedulingPreviewKey struct{}

// Preview uses the exact policy/eligibility path without advancing rotation,
// creating affinity, reserving capacity, refreshing quota or calling providers.
// It describes unbound Responses placement before key-specific allowances.
func (s *Service) Scheduling(ctx context.Context, userID, groupID int64, model string) (SchedulingPreview, error) {
	if len(model) > 160 || model != "" && !safeModel.MatchString(model) {
		return SchedulingPreview{}, upstream.ErrInput
	}
	provider, native := groups.SplitModel(model)
	if len(native) > 128 {
		return SchedulingPreview{}, upstream.ErrInput
	}
	result := SchedulingPreview{Dynamic: s.policy.QuotaWeighted, Weights: []SchedulingWeight{}, Route: &RouteExplanation{}}
	ctx = context.WithValue(ctx, routeKey{}, result.Route)
	ctx = context.WithValue(ctx, schedulingPreviewKey{}, &result)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, err := s.selectAccount(ctx, userID, groupID, "", provider, native, Responses)
	// A valid preview can have no eligible resource. Keep the recorded exclusion
	// categories instead of inventing a target share for unavailable accounts.
	if err != nil && !errors.Is(err, ErrNoAccount) && !errors.Is(err, ErrAccountBusy) && !errors.Is(err, ErrAccountCooling) && !errors.Is(err, ErrModelCooling) && !errors.Is(err, ErrQuotaExhausted) && !errors.Is(err, ErrCatalogUnavailable) && !errors.Is(err, ErrModelUnavailable) && !errors.Is(err, ErrModelNotAllowed) {
		return SchedulingPreview{}, err
	}
	return result, nil
}
