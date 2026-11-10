package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

// Called under s.mu by Open, keeping policy, selection and account reservation atomic.
func (s *Service) selectAccount(ctx context.Context, userID, groupID int64, session, provider, model string, kind Kind) (string, [32]byte, error) {
	return s.selectAllocationAccount(ctx, userID, groupID, session, provider, model, kind, 0, pinnedAffinity, "")
}
func (s *Service) selectAllocationAccount(ctx context.Context, userID, groupID int64, session, provider, model string, kind Kind, scheme int64, affinity affinityMode, parentThread string) (selected string, digest [32]byte, failure error) {
	digest = sessionDigest(userID, groupID, session)
	route, _ := ctx.Value(routeKey{}).(*RouteExplanation)
	if route == nil {
		route = &RouteExplanation{}
	}
	// Queued attempts replace the previous snapshot; counts describe one actual selection.
	*route = RouteExplanation{Version: 1, Strategy: "none", Result: "rejected"}
	defer func() {
		if failure == nil {
			route.Result = "selected"
		} else if route.Strategy == "affinity" {
			route.Exclusions.reject(failure)
		}
	}()
	if userID <= 0 || groupID <= 0 {
		return "", digest, upstream.ErrInput
	}
	if provider != "" && !s.accounts.ProviderEnabled(provider) {
		return "", digest, accounts.ErrProviderDisabled
	}
	if err := s.loadRuntime(ctx); err != nil {
		return "", digest, err
	}
	if session == "" {
		if _, err := rand.Read(digest[:]); err != nil {
			return "", digest, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", digest, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	schedulingPolicy, err := groups.ReadSchedulingPolicy(ctx, q, s.tenantID)
	if err != nil {
		return "", digest, err
	}
	activePolicy := schedulingPolicy
	if draft, ok := ctx.Value(schedulingDraftKey{}).(groups.SchedulingPolicy); ok {
		if draft.Revision != schedulingPolicy.Revision {
			return "", digest, groups.ErrSchedulingConflict
		}
		schedulingPolicy = draft
	}
	if preview, ok := ctx.Value(schedulingPreviewKey{}).(*SchedulingPreview); ok {
		preview.Policy = schedulingPolicy
	}
	rows, err := q.ListRoutingAccounts(ctx, db.ListRoutingAccountsParams{TenantID: s.tenantID, GroupID: groupID})
	if err != nil {
		return "", digest, err
	}
	available := make([]accounts.Account, 0, len(rows))
	preview, _ := ctx.Value(schedulingPreviewKey{}).(*SchedulingPreview)
	statuses := make(map[string]*SchedulingResource)
	if preview != nil {
		preview.Resources = make([]SchedulingResource, 0, len(rows))
	}
	mark := func(id string, err error) {
		if v := statuses[id]; v != nil {
			switch {
			case err == nil:
				v.State = "eligible"
			case errors.Is(err, ErrAccountBusy):
				v.State = "busy"
			case errors.Is(err, ErrAccountCooling), errors.Is(err, ErrModelCooling):
				v.State = "cooling"
			case errors.Is(err, ErrQuotaExhausted):
				v.State = "quota"
			case errors.Is(err, ErrCatalogUnavailable):
				v.State = "catalog_unknown"
			case errors.Is(err, ErrModelUnavailable):
				v.State = "model"
			default:
				v.State = "policy"
			}
		}
	}
	snapshots := make(map[string]db.ListRoutingAccountsRow, len(rows))
	for _, row := range rows {
		available = append(available, accounts.Account{ID: row.ID, Provider: row.Provider, Enabled: row.Enabled, Status: row.Status})
		snapshots[row.ID] = row
		if preview != nil {
			state := s.health[row.ID]
			v := &SchedulingResource{AccountID: row.ID, State: "policy", Tier: 4}
			if state != nil {
				v.InFlight = state.InFlight
				v.MaxConcurrency = state.MaxConcurrency
			}
			statuses[row.ID] = v
		}
	}
	defer func() {
		if preview != nil {
			for _, row := range rows {
				preview.Resources = append(preview.Resources, *statuses[row.ID])
			}
		}
	}()
	// Catalogs, quota and pool policy share this transaction; no decrypted management metadata is needed.
	supports := func(id, model, provider string) (bool, bool, error) {
		row := snapshots[id]
		value, err := accounts.DecodeCatalog(row.ModelsSnapshot, row.ModelsRevision)
		if err != nil {
			return false, false, err
		}
		known, supported := s.catalogSupports(value, model, provider)
		return known, supported, nil
	}
	quotaAdmission := func(account accounts.Account, model string) error {
		row := snapshots[account.ID]
		decision := s.snapshotQuota(account.Provider, model, row.UsageSnapshot, row.UsageUpdatedAt)
		if decision.State == "exhausted" {
			return &QuotaError{RetryAfter: max(1, decision.RetryAt-s.now().Unix())}
		}
		return nil
	}
	allowed, err := poolAccounts(ctx, q, userID, groupID)
	if err != nil {
		return "", digest, err
	}
	policy, err := groups.ReadPolicy(ctx, q, userID, groupID)
	if err != nil {
		return "", digest, err
	}
	requested := model
	if provider != "" {
		requested = provider + "/" + model
	}
	if model != "" && !policy.Allows(requested) {
		route.Exclusions.Policy = min(maxRouteCount, len(available))
		return "", digest, ErrModelNotAllowed
	}
	scope := provider
	// Automatic routing needs one binding across providers so a new catalog or cooldown cannot move a conversation.
	if scope == "" {
		scope = "auto"
	}
	now := s.now().Unix()
	expires := now + 24*3600
	bindingModel := upstream.CatalogModelID(model)
	var replacement *db.ListSessionAffinitiesRow
	if session != "" {
		existing, err := sessionBinding(ctx, q, db.ListSessionAffinitiesParams{GroupID: groupID, UserID: userID, SessionHash: digest[:], Now: now}, scope)
		if err != nil {
			return "", digest, err
		}
		if existing != nil {
			route.Strategy = "affinity"
			id := existing.AccountID
			if !allowed[id] {
				return id, digest, ErrAffinityUnavailable
			}
			var bound *accounts.Account
			for i := range available {
				if available[i].ID == id {
					bound = &available[i]
					break
				}
			}
			if bound == nil {
				return id, digest, accounts.ErrNotFound
			}
			if !s.accounts.ProviderEnabled(bound.Provider) {
				return id, digest, accounts.ErrProviderDisabled
			}
			if !policy.AllowsProvider(bound.Provider) {
				return id, digest, ErrAffinityUnavailable
			}
			if kind == Compact && bound.Provider != "codex" {
				return id, digest, upstream.ErrInput
			}
			reselect := false
			if model != "" && bound.Enabled && bound.Status != "reauth_required" {
				// A model change alone does not change account identity. After a recorded
				// account move, older opaque state must still stay off the new account.
				if affinity == continuationAffinity && existing.Rebound && existing.Model != "" && bindingModel != existing.Model {
					return id, digest, ErrModelConflict
				}
				known, supported, err := supports(id, model, bound.Provider)
				if err != nil {
					return id, digest, err
				}
				if !known {
					return id, digest, ErrCatalogUnavailable
				}
				if !supported {
					// A missing capability is not a failover signal. Only an explicit
					// model change with portable history may replace a known binding.
					if affinity != portableAffinity || existing.Model == "" || existing.Model == bindingModel {
						return id, digest, ErrModelConflict
					}
					reselect = true
				}
			}
			if reselect {
				replacement = existing
			} else {
				if model != "" && !policy.AllowsResource(bound.Provider, model) {
					return id, digest, ErrModelNotAllowed
				}
				if err := s.accountAdmission(*bound); err != nil {
					return id, digest, err
				}
				if err := s.modelAdmission(ctx, q, id, model); err != nil {
					return id, digest, err
				}
				if err := quotaAdmission(*bound, model); err != nil {
					return id, digest, err
				}
				if scheme != 0 {
					if _, err := allocations.Check(ctx, q, scheme, userID, groupID, id, model, now, s.location()); err != nil {
						return id, digest, err
					}
				}
				if bindingModel == "" {
					bindingModel = existing.Model
				}
				if existing.Provider != scope {
					if err := rememberAffinity(ctx, q, userID, groupID, digest, scope, id, bindingModel, now, expires, existing.Rebound); err != nil {
						return "", digest, err
					}
				}
				if err := q.TouchAccountAffinity(ctx, db.TouchAccountAffinityParams{GroupID: groupID, Provider: existing.Provider, UserID: userID, SessionHash: digest[:], Model: bindingModel, ExpiresAt: expires, Threshold: expires - 1800}); err != nil {
					return "", digest, err
				}
				if err := tx.Commit(); err != nil {
					return "", digest, err
				}
				route.Candidates = 1
				return id, digest, nil
			}
		}
	}
	// A portable model change resumes scheduling rather than retaining the previous binding.
	route.Strategy = "none"
	route.ParentFallback = parentThread != "" && affinity != continuationAffinity
	parentAccount := ""
	if parentThread != "" && affinity != continuationAffinity {
		// Resolve only in the authenticated user's pool; parent hints never contain trusted account IDs.
		hash := sessionDigest(userID, groupID, parentThread)
		binding, err := sessionBinding(ctx, q, db.ListSessionAffinitiesParams{GroupID: groupID, UserID: userID, SessionHash: hash[:], Now: now}, "auto")
		if err != nil && !errors.Is(err, ErrAffinityUnavailable) {
			return "", digest, err
		}
		if binding != nil {
			parentAccount = binding.AccountID
		}
	}
	candidates := make([]string, 0, len(available))
	preferred := preferredProvider(kind)
	bestRank := 4
	busy, unknown, eligible := false, false, false
	var cooling, quotaWait int64
	var coolingModel string
	var allocationErr error
	for _, account := range available {
		if !s.accounts.ProviderEnabled(account.Provider) || !policy.AllowsProvider(account.Provider) || !allowed[account.ID] || !account.Enabled || account.Status == "reauth_required" || (provider != "" && account.Provider != provider) || (kind == Compact && account.Provider != "codex") {
			if !s.accounts.ProviderEnabled(account.Provider) || !account.Enabled || account.Status == "reauth_required" {
				if v := statuses[account.ID]; v != nil {
					v.State = "unavailable"
				}
				route.Exclusions.Unavailable = min(maxRouteCount, route.Exclusions.Unavailable+1)
			} else {
				route.Exclusions.Policy = min(maxRouteCount, route.Exclusions.Policy+1)
			}
			continue
		}
		if model != "" && !policy.AllowsResource(account.Provider, model) {
			route.Exclusions.Policy = min(maxRouteCount, route.Exclusions.Policy+1)
			continue
		}
		eligible = true
		if model != "" {
			known, supported, err := supports(account.ID, model, account.Provider)
			if err != nil {
				return "", digest, err
			}
			if !known {
				mark(account.ID, ErrCatalogUnavailable)
				route.Exclusions.Model = min(maxRouteCount, route.Exclusions.Model+1)
				unknown = true
				continue
			}
			if !supported {
				mark(account.ID, ErrModelUnavailable)
				route.Exclusions.Model = min(maxRouteCount, route.Exclusions.Model+1)
				continue
			}
		}
		if err := s.accountAdmission(account); err != nil {
			mark(account.ID, err)
			route.Exclusions.reject(err)
			if errors.Is(err, ErrAccountBusy) {
				busy = true
			}
			var wait *CoolingError
			if errors.As(err, &wait) && (cooling == 0 || wait.RetryAfter < cooling) {
				cooling = wait.RetryAfter
				coolingModel = ""
			}
			continue
		}
		if err := quotaAdmission(account, model); err != nil {
			mark(account.ID, err)
			route.Exclusions.reject(err)
			var quota *QuotaError
			if !errors.As(err, &quota) {
				return "", digest, err
			}
			if quotaWait == 0 || quota.RetryAfter < quotaWait {
				quotaWait = quota.RetryAfter
			}
			continue
		}
		if err := s.modelAdmission(ctx, q, account.ID, model); err != nil {
			mark(account.ID, err)
			route.Exclusions.reject(err)
			if errors.Is(err, ErrAccountBusy) {
				busy = true
				continue
			}
			var wait *CoolingError
			if errors.As(err, &wait) {
				if cooling == 0 || wait.RetryAfter < cooling {
					cooling = wait.RetryAfter
					coolingModel = wait.Model
				}
				continue
			}
			return "", digest, err
		}
		if scheme != 0 {
			if _, err := allocations.Check(ctx, q, scheme, userID, groupID, account.ID, model, now, s.location()); err != nil {
				if !errors.Is(err, allocations.ErrQuota) && !errors.Is(err, allocations.ErrPending) && !errors.Is(err, allocations.ErrRisk) && !errors.Is(err, allocations.ErrUnpriced) && !errors.Is(err, allocations.ErrUnavailable) {
					return "", digest, err
				}
				route.Exclusions.reject(err)
				allocationErr = err
				continue
			}
		}
		// Preference only ranks accounts that passed every policy and availability check.
		// Existing affinities returned above must never move to a more direct provider.
		rank := policy.Routing.Rank(account.Provider, preferred)
		mark(account.ID, nil)
		if v := statuses[account.ID]; v != nil {
			v.Tier = rank
		}
		// Parent metadata is only a preference among admitted accounts, never a binding or permission grant.
		if account.ID == parentAccount {
			rank = -1
		}
		if rank < bestRank {
			candidates = candidates[:0]
			bestRank = rank
		}
		if rank == bestRank {
			candidates = append(candidates, account.ID)
		}
	}
	for _, v := range statuses {
		if v.State == "eligible" && v.Tier > bestRank {
			v.State = "lower_tier"
		}
	}
	if len(candidates) == 0 {
		if allocationErr != nil {
			return "", digest, allocationErr
		}
		if busy {
			return "", digest, ErrAccountBusy
		}
		if cooling > 0 {
			return "", digest, &CoolingError{RetryAfter: cooling, Model: coolingModel}
		}
		if quotaWait > 0 {
			return "", digest, &QuotaError{RetryAfter: quotaWait}
		}
		if unknown {
			return "", digest, ErrCatalogUnavailable
		}
		if eligible && model != "" {
			return "", digest, ErrModelUnavailable
		}
		return "", digest, ErrNoAccount
	}
	route.Strategy = "rotation"
	route.Candidates = min(maxRouteCount, len(candidates))
	// Isolate rotation across preference tiers so traffic using another protocol cannot starve this pool.
	cursor := fmt.Sprintf("%d:%s:%s:%s:%d", groupID, scope, policy.Routing.Preference, preferred, bestRank)
	weighted, details := s.candidateWeights(candidates, snapshots, model, schedulingPolicy)
	if s.policy.QuotaWeighted {
		route.Strategy = "quota_weighted"
	}
	if preview, ok := ctx.Value(schedulingPreviewKey{}).(*SchedulingPreview); ok {
		if _, draft := ctx.Value(schedulingDraftKey{}).(groups.SchedulingPolicy); draft {
			_, preview.Baseline = s.candidateWeights(candidates, snapshots, model, activePolicy)
		}
		preview.Weights = details
		return candidates[0], digest, nil
	}
	id := candidates[0]
	if id != parentAccount {
		if s.policy.QuotaWeighted {
			route.Strategy = "quota_weighted"
		}
		if s.policy.QuotaWeighted {
			cursor += ":" + upstream.CatalogModelID(model)
		}
		id = s.scheduler.choose(cursor, weighted)
	}
	if id == parentAccount {
		route.Strategy = "parent"
		route.ParentFallback = false
	}
	if session != "" {
		if replacement != nil && replacement.Provider == scope {
			err = q.ReplaceAccountAffinity(ctx, db.ReplaceAccountAffinityParams{UserID: userID, GroupID: groupID, SessionHash: digest[:], Provider: scope, AccountID: id, Model: bindingModel, ExpiresAt: expires})
		} else {
			// Creating an auto binding from a legacy scope can also move the account.
			err = rememberAffinity(ctx, q, userID, groupID, digest, scope, id, bindingModel, now, expires, replacement != nil)
		}
		if err != nil {
			return "", digest, err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", digest, err
	}
	if id != parentAccount {
		s.recordSchedulingPlacement(groupID, cursor, schedulingPolicy.Revision, id, details)
	}
	return id, digest, nil
}

func preferredProvider(kind Kind) string {
	switch kind {
	case Messages:
		return "claude"
	case Gemini, GeminiStream:
		return "antigravity"
	default:
		return "codex"
	}
}

func rememberAffinity(ctx context.Context, q *db.Queries, userID, groupID int64, digest [32]byte, scope, id, model string, now, expires int64, rebound bool) error {
	if err := q.PruneAccountAffinity(ctx, now); err != nil {
		return err
	}
	count, err := q.CountAccountAffinity(ctx)
	if err != nil {
		return err
	}
	if count >= 4096 {
		return ErrAffinityLimit
	}
	return q.CreateAccountAffinity(ctx, db.CreateAccountAffinityParams{GroupID: groupID, Provider: scope, UserID: userID, SessionHash: digest[:], AccountID: id, Model: model, ExpiresAt: expires, Rebound: rebound})
}
