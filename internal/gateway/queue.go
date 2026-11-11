package gateway

import (
	"context"
	"database/sql"
	"errors"

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

// Selection, affinity replacement and reservation share the admission lock. No request waits for capacity.
func (s *Service) leaseAccount(ctx context.Context, entry *observation, session, provider, model string, kind Kind, affinity affinityMode, parentThread string, prepare ...func(string) error) (id string, digest [32]byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil && entry.route != nil && entry.route.Result == "selected" {
			entry.route.Result = "rejected"
			entry.route.Exclusions.reject(err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return id, digest, err
	}
	if s.closed {
		return id, digest, context.Canceled
	}
	if err := s.prepareAllocation(ctx, entry); err != nil {
		return id, digest, err
	}
	if err := s.recheckMember(ctx, s.queries, entry); err != nil {
		return id, digest, err
	}
	if err := s.admitMember(ctx, entry.record.UserID); err != nil {
		return id, digest, err
	}
	entry.memberLeased = true
	seen := make(map[string]bool)
	for {
		id, digest, err = s.selectAllocationAccount(ctx, entry.record.UserID, entry.record.GroupID, session, provider, model, kind, entry.schemeID, affinity, parentThread)
		entry.record.AccountID = id
		if err != nil {
			return id, digest, err
		}
		if err := ctx.Err(); err != nil {
			return id, digest, err
		}
		modelState, err := s.modelLease(ctx, id, model)
		if err != nil {
			return id, digest, err
		}
		state := s.health[id]
		state.InFlight++
		if modelState != nil {
			modelState.inFlight++
			s.modelHealth[modelState.key] = modelState
			entry.modelState = modelState
		}
		s.sequence++
		entry.sequence = s.sequence
		entry.revision = state.revision
		entry.leased = true
		if len(prepare) > 0 && prepare[0] != nil {
			// Credential IO owns a reserved account slot, but never the gateway mutex or a database transaction.
			s.mu.Unlock()
			prepareErr := prepare[0](id)
			s.mu.Lock()
			if prepareErr != nil {
				state.InFlight = max(0, state.InFlight-1)
				if modelState != nil {
					modelState.inFlight = max(0, modelState.inFlight-1)
					if modelState.inFlight == 0 && !modelState.persistFailed && s.modelHealth[modelState.key] == modelState {
						delete(s.modelHealth, modelState.key)
					}
				}
				entry.modelState = nil
				entry.leased = false
				if !replaceUnavailableAffinity(prepareErr) || seen[id] || len(seen) >= maxModelRuntime {
					return id, digest, prepareErr
				}
				seen[id] = true
				continue
			}
		}

		if s.closed {
			return id, digest, context.Canceled
		}
		if entry.schemeID != 0 {
			tx, beginErr := s.db.BeginTx(ctx, nil)
			if beginErr != nil {
				return id, digest, beginErr
			}
			defer tx.Rollback()
			admittedAt := s.now().Unix()
			if err := allocations.Begin(ctx, s.queries.WithTx(tx), allocations.Request{ID: entry.record.RequestID, SchemeID: entry.schemeID, UserID: entry.record.UserID, GroupID: entry.record.GroupID, AccountID: id, Model: model, StartedAt: admittedAt}, admittedAt, s.location()); err != nil {
				return id, digest, err
			}
			if err := tx.Commit(); err != nil {
				return id, digest, err
			}
			entry.allocationTracked = true
		}
		return id, digest, nil
	}
}

func (s *Service) recheckMember(ctx context.Context, q *db.Queries, entry *observation) error {
	policy, err := q.GetMemberLimits(ctx, db.GetMemberLimitsParams{TenantID: s.tenantID, UserID: entry.record.UserID})
	if errors.Is(err, sql.ErrNoRows) || err == nil && !policy.Enabled {
		return groups.ErrUnavailable
	}
	if err != nil {
		return err
	}
	// Rechecks during retry already own a member lease; initial admission reserves it below.
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
