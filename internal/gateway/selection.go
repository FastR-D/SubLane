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
	rows, err := q.ListRoutingAccounts(ctx, db.ListRoutingAccountsParams{TenantID: s.tenantID, GroupID: groupID})
	if err != nil {
		return "", digest, err
	}
	available := make([]accounts.Account, 0, len(rows))
	snapshots := make(map[string]db.ListRoutingAccountsRow, len(rows))
	for _, row := range rows {
		available = append(available, accounts.Account{ID: row.ID, Provider: row.Provider, Enabled: row.Enabled, Status: row.Status})
		snapshots[row.ID] = row
	}
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
	derived := false
	if hints, ok := stickyFrom(ctx); ok {
		sticky := policy.Routing.SessionSticky
		bindingScope := provider
		if bindingScope == "" {
			bindingScope = "auto"
		}
		// bound reports whether a key already owns a binding; ambiguous legacy bindings count as bound.
		bound := func(hash [32]byte) (bool, error) {
			binding, err := sessionBinding(ctx, q, db.ListSessionAffinitiesParams{GroupID: groupID, UserID: userID, SessionHash: hash[:], Now: s.now().Unix()}, bindingScope)
			if errors.Is(err, ErrAffinityUnavailable) {
				return true, nil
			}
			return binding != nil, err
		}
		normalize := false
		switch {
		case hints.Session == "" || session != "" && !hints.Replace:
		case hints.Derived:
			// Preferences are the opt-in feature itself and disappear with the setting.
			normalize = sticky
		default:
			// Explicit Claude conversations keep whichever key already holds their binding, whether the
			// setting is on or off, so provider-held state never moves when the toggle changes.
			rawBound := false
			if session != "" {
				if rawBound, err = bound(digest); err != nil {
					return "", digest, err
				}
			}
			if !rawBound {
				normalizedBound, err := bound(sessionDigest(userID, groupID, hints.Session))
				if err != nil {
					return "", digest, err
				}
				// The setting only decides whether an unbound conversation creates a normalized binding.
				normalize = normalizedBound || sticky
			}
		}
		if normalize {
			session, derived = hints.Session, hints.Derived
			digest = sessionDigest(userID, groupID, session)
			if derived {
				// Preferences live in their own namespace so they can never read or rewrite an explicit binding.
				digest = derivedDigest(userID, groupID, session)
			}
		}
		// Subagents prefer their parent's resource among admitted candidates, as Codex reviews already do.
		if sticky && parentThread == "" && hints.Parent != "" && hints.Parent != session {
			parentThread = hints.Parent
		}
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
	if derived {
		expires = now + derivedAffinitySeconds
	}
	bindingModel := upstream.CatalogModelID(model)
	var replacement *db.ListSessionAffinitiesRow
	stickyAccount := ""
	if session != "" && derived {
		// A derived binding is only a preference: unavailable resources fall back to normal scheduling.
		existing, err := sessionBinding(ctx, q, db.ListSessionAffinitiesParams{GroupID: groupID, UserID: userID, SessionHash: digest[:], Now: now}, scope)
		if err != nil && !errors.Is(err, ErrAffinityUnavailable) {
			return "", digest, err
		}
		if existing != nil {
			stickyAccount, replacement = existing.AccountID, existing
		}
	}
	if session != "" && !derived {
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
			if kind.codexOnly() && bound.Provider != "codex" {
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
					if err := rememberAffinity(ctx, q, userID, groupID, digest, scope, id, bindingModel, now, expires, existing.Rebound, false); err != nil {
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
		// Parent lookups are read-only: an explicit parent binding wins, then a derived parent preference.
		hashes := [][32]byte{sessionDigest(userID, groupID, parentThread)}
		if policy.Routing.SessionSticky {
			hashes = append(hashes, derivedDigest(userID, groupID, parentThread))
		}
		for _, hash := range hashes {
			binding, err := sessionBinding(ctx, q, db.ListSessionAffinitiesParams{GroupID: groupID, UserID: userID, SessionHash: hash[:], Now: now}, "auto")
			if err != nil && !errors.Is(err, ErrAffinityUnavailable) {
				return "", digest, err
			}
			if binding != nil {
				parentAccount = binding.AccountID
				break
			}
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
		if !s.accounts.ProviderEnabled(account.Provider) || !policy.AllowsProvider(account.Provider) || !allowed[account.ID] || !account.Enabled || account.Status == "reauth_required" || (provider != "" && account.Provider != provider) || (kind.codexOnly() && account.Provider != "codex") {
			if !s.accounts.ProviderEnabled(account.Provider) || !account.Enabled || account.Status == "reauth_required" {
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
				route.Exclusions.Model = min(maxRouteCount, route.Exclusions.Model+1)
				unknown = true
				continue
			}
			if !supported {
				route.Exclusions.Model = min(maxRouteCount, route.Exclusions.Model+1)
				continue
			}
		}
		if err := s.accountAdmission(account); err != nil {
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
		// Parent metadata is only a preference among admitted accounts, never a binding or permission grant.
		if account.ID == parentAccount {
			rank = -1
		}
		// The conversation's own sticky resource outranks its parent's.
		if account.ID == stickyAccount {
			rank = -2
		}
		if rank < bestRank {
			candidates = candidates[:0]
			bestRank = rank
		}
		if rank == bestRank {
			candidates = append(candidates, account.ID)
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
	id := candidates[s.next[cursor]%len(candidates)]
	period := len(candidates)
	if s.policy.QuotaWeighted && len(candidates) > 1 {
		weights := make([]int, len(candidates))
		total := 0
		for i, candidate := range candidates {
			row := snapshots[candidate]
			decision := s.snapshotQuota(row.Provider, model, row.UsageSnapshot, row.UsageUpdatedAt)
			// Percentages across providers are incomparable; partial observations keep equal rotation.
			if row.Provider != snapshots[candidates[0]].Provider || decision.Weight == 0 {
				total = 0
				break
			}
			weights[i] = decision.Weight
			total += decision.Weight
		}
		if total > 0 {
			route.Strategy = "quota_weighted"
			period = total
			ticket := s.next[cursor] % total
			for i, weight := range weights {
				if ticket < weight {
					id = candidates[i]
					break
				}
				ticket -= weight
			}
		}
	}
	if id == parentAccount {
		route.Strategy = "parent"
		route.ParentFallback = false
	}
	if stickyAccount != "" && id == stickyAccount {
		route.Strategy = "affinity"
		route.ParentFallback = false
	}
	s.next[cursor] = (s.next[cursor] + 1) % period
	if session != "" {
		if replacement != nil && replacement.Provider == scope {
			err = q.ReplaceAccountAffinity(ctx, db.ReplaceAccountAffinityParams{UserID: userID, GroupID: groupID, SessionHash: digest[:], Provider: scope, AccountID: id, Model: bindingModel, ExpiresAt: expires})
		} else {
			// Creating an auto binding from a legacy scope can also move the account.
			err = rememberAffinity(ctx, q, userID, groupID, digest, scope, id, bindingModel, now, expires, replacement != nil && !derived, derived)
		}
		// Derived preferences must never reject traffic that would otherwise be scheduled.
		if derived && errors.Is(err, ErrAffinityLimit) {
			err = nil
		}
		if err != nil {
			return "", digest, err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", digest, err
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

const (
	affinityLimit = 4096
	// Derived preferences may hold at most a quarter of the shared table.
	derivedAffinityLimit = affinityLimit / 4
)

func rememberAffinity(ctx context.Context, q *db.Queries, userID, groupID int64, digest [32]byte, scope, id, model string, now, expires int64, rebound, derived bool) error {
	if err := q.PruneAccountAffinity(ctx, now); err != nil {
		return err
	}
	count, err := q.CountAccountAffinity(ctx)
	if err != nil {
		return err
	}
	if derived {
		preferences, err := q.CountDerivedAffinity(ctx)
		if err != nil {
			return err
		}
		if count >= affinityLimit || preferences >= derivedAffinityLimit {
			return ErrAffinityLimit
		}
	} else if count >= affinityLimit {
		// An explicit conversation never fails because preferences filled the table.
		evicted, err := q.EvictDerivedAffinity(ctx)
		if err != nil {
			return err
		}
		if evicted == 0 {
			return ErrAffinityLimit
		}
	}
	return q.CreateAccountAffinity(ctx, db.CreateAccountAffinityParams{GroupID: groupID, Provider: scope, UserID: userID, SessionHash: digest[:], AccountID: id, Model: model, ExpiresAt: expires, Rebound: rebound, Derived: derived})
}
