package gateway

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
)

type accountWaitError string

func (e accountWaitError) Error() string { return string(e) }
func (e accountWaitError) Unwrap() error { return ErrAccountBusy }

var (
	ErrAccountQueueFull   = accountWaitError("account_queue_full")
	ErrAccountWaitTimeout = accountWaitError("account_wait_timeout")
)

// Selection and leasing share the admission lock, but waiting holds neither a lock nor a transaction.
func (s *Service) leaseAccount(ctx context.Context, entry *observation, session, provider, model string, kind Kind, affinity affinityMode, parentThread string) (id string, digest [32]byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var timer *time.Timer
	var retry *time.Ticker
	var deadline time.Time
	var waitedAt time.Time
	admissionCtx := ctx
	defer func() {
		if timer != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			} else if err != nil && errors.Is(admissionCtx.Err(), context.DeadlineExceeded) {
				err = ErrAccountWaitTimeout
			}
			timer.Stop()
			retry.Stop()
			s.waiting[entry.record.GroupID]--
			if s.waiting[entry.record.GroupID] == 0 {
				delete(s.waiting, entry.record.GroupID)
				delete(s.capacityChanged, entry.record.GroupID)
			}
		}
		// Selection may succeed before the model lease or allowance reservation fails.
		if err != nil && entry.route != nil && entry.route.Result == "selected" {
			entry.route.Result = "rejected"
			entry.route.Exclusions.reject(err)
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return id, digest, err
		}
		if s.closed {
			return id, digest, context.Canceled
		}
		if timer != nil && !time.Now().Before(deadline) {
			return id, digest, ErrAccountWaitTimeout
		}
		if err := s.prepareAllocation(admissionCtx, entry); err != nil {
			return id, digest, err
		}
		if !entry.memberLeased {
			if err := s.admitMember(admissionCtx, entry.record.UserID); err != nil {
				return id, digest, err
			}
			entry.memberLeased = true
		} else if err := s.recheckWaitingMember(admissionCtx, entry); err != nil {
			return id, digest, err
		}
		id, digest, err = s.selectAllocationAccount(admissionCtx, entry.record.UserID, entry.record.GroupID, session, provider, model, kind, entry.schemeID, affinity, parentThread)
		entry.record.AccountID = id
		if err != ErrAccountBusy {
			break
		}
		if timer == nil {
			if s.waiting[entry.record.GroupID] >= s.policy.QueueLimit {
				return id, digest, ErrAccountQueueFull
			}
			s.waiting[entry.record.GroupID]++
			waitedAt = time.Now()
			deadline = time.Now().Add(s.accountWaitTimeout)
			waitingCtx, stopWaiting := context.WithDeadline(ctx, deadline)
			defer stopWaiting()
			admissionCtx = waitingCtx
			timer = time.NewTimer(s.accountWaitTimeout)
			// Lifecycle/policy edits happen outside the gateway lock. Recheck them even without a lease release.
			retry = time.NewTicker(250 * time.Millisecond)
		}
		changed := s.capacityChanged[entry.record.GroupID]
		if changed == nil {
			changed = make(chan struct{}, 1)
			s.capacityChanged[entry.record.GroupID] = changed
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-timer.C:
		case <-retry.C:
		case <-changed:
		}
		s.mu.Lock()
	}
	if err != nil {
		return id, digest, err
	}
	if err := admissionCtx.Err(); err != nil {
		return id, digest, err
	}
	modelState, err := s.modelLease(admissionCtx, id, model)
	if err != nil {
		return id, digest, err
	}
	if entry.schemeID != 0 {
		tx, beginErr := s.db.BeginTx(admissionCtx, nil)
		if beginErr != nil {
			return id, digest, beginErr
		}
		defer tx.Rollback()
		// Queue time stays in diagnostics; allowance usage belongs to successful admission.
		admittedAt := s.now().Unix()
		if err := allocations.Begin(admissionCtx, s.queries.WithTx(tx), allocations.Request{ID: entry.record.RequestID, SchemeID: entry.schemeID, UserID: entry.record.UserID, GroupID: entry.record.GroupID, AccountID: id, Model: model, StartedAt: admittedAt}, admittedAt, s.location()); err != nil {
			return id, digest, err
		}
		if err := tx.Commit(); err != nil {
			return id, digest, err
		}
		entry.allocationTracked = true
	}
	state := s.health[id]
	if !waitedAt.IsZero() {
		entry.waitMS = time.Since(waitedAt).Milliseconds()
	}
	state.InFlight++
	if modelState != nil {
		modelState.inFlight++
		s.modelHealth[modelState.key] = modelState
		entry.modelState = modelState
	}
	// Arrival can precede a failure while queued; only an admitted request may count as its recovery.
	s.sequence++
	entry.sequence = s.sequence
	entry.revision = state.revision
	entry.leased = true
	return id, digest, nil
}

func (s *Service) recheckWaitingMember(ctx context.Context, entry *observation) error {
	return s.recheckMember(ctx, s.queries, entry)
}

func (s *Service) recheckMember(ctx context.Context, q *db.Queries, entry *observation) error {
	policy, err := q.GetMemberLimits(ctx, db.GetMemberLimitsParams{TenantID: s.tenantID, UserID: entry.record.UserID})
	if errors.Is(err, sql.ErrNoRows) || err == nil && !policy.Enabled {
		return groups.ErrUnavailable
	}
	if err != nil {
		return err
	}
	// The waiting request already owns a member lease and consumes RPM only once.
	if policy.MaxConcurrency > 0 && s.memberActive[entry.record.UserID] > policy.MaxConcurrency {
		return ErrMemberBusy
	}
	if entry.record.KeyID != 0 {
		key, err := q.GetKey(ctx, db.GetKeyParams{ID: entry.record.KeyID, UserID: entry.record.UserID, TenantID: s.tenantID})
		if errors.Is(err, sql.ErrNoRows) {
			return groups.ErrUnavailable
		}
		if err != nil {
			return err
		}
		if !key.Enabled || key.RevokedAt != nil || key.ExpiresAt != nil && *key.ExpiresAt <= s.now().Unix() || key.GroupID != entry.record.GroupID {
			return groups.ErrUnavailable
		}
	}
	return nil
}

// Call under s.mu after accounting commits, so awakened requests see the final charge.
func (s *Service) notifyCapacity(accountID string) {
	if len(s.waiting) == 0 {
		return
	}
	groups, err := s.queries.ListResourceGroups(s.runContext, db.ListResourceGroupsParams{TenantID: s.tenantID, AccountID: accountID})
	if err != nil {
		// A failed lookup must not strand requests whose capacity was just released.
		for groupID := range s.waiting {
			groups = append(groups, groupID)
		}
	}
	for _, groupID := range groups {
		if changed := s.capacityChanged[groupID]; changed != nil {
			// Coalesce releases and wake one waiter per affected pool; policy polling remains independent.
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	}
}
