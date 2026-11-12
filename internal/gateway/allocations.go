package gateway

import (
	"context"
	"database/sql"
	"errors"

	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/storage/db"
)

var ErrAllocationAccounting = errors.New("allocation_accounting_unavailable")

func (s *Service) Allocations() *allocations.Service {
	service := allocations.NewForTenantWithPricing(s.db, s.tenantID, s.pricing)
	service.SetLocation(s.location)
	return service
}

func (e *observation) settleAllocation(ctx context.Context, q *db.Queries) error {
	if !e.allocationTracked {
		return nil
	}
	value := func(v *int64) int64 {
		if v != nil {
			return *v
		}
		return 0
	}
	known := e.record.InputTokens != nil && e.record.OutputTokens != nil && (e.usageFinal || e.record.Outcome == "success" || e.record.Outcome == "incomplete")
	rejected := e.record.UpstreamStatus != nil && *e.record.UpstreamStatus >= 400
	err := allocations.Finish(ctx, q, e.record.RequestID, allocations.Completion{Input: value(e.record.InputTokens), ImageInput: e.imageInputTokens, Output: value(e.record.OutputTokens), Cached: value(e.record.CachedTokens), Known: known, Dispatched: e.upstreamDispatched, Rejected: rejected}, e.service.now().UnixMilli(), false)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAllocationAccounting
	}
	return err
}

func (s *Service) PrepareAllocations(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoverAllocations(ctx)
}

func (s *Service) prepareAllocation(ctx context.Context, e *observation) error {
	if err := s.ensureAllocationsReady(ctx); err != nil {
		return err
	}
	scheme, err := allocations.KeyScheme(ctx, s.queries, e.record.KeyID, e.record.UserID, e.record.GroupID, s.now().Unix())
	if err != nil {
		return err
	}
	e.schemeID = scheme
	if err := s.retryFailedSettlements(ctx); err != nil {
		for _, failed := range s.failedSettlements {
			if failed.schemeID == scheme && failed.record.UserID == e.record.UserID {
				return ErrAllocationAccounting
			}
		}
	}
	return nil
}

// Startup recovery is global; live write failures retry their own requests only.
func (s *Service) recoverAllocations(ctx context.Context) error {
	if err := s.ensureAllocationsReady(ctx); err != nil {
		return err
	}
	if err := s.retryFailedSettlements(ctx); err != nil {
		return ErrAllocationAccounting
	}
	return nil
}

func (s *Service) ensureAllocationsReady(ctx context.Context) error {
	if s.allocationsReady {
		return nil
	}
	// Only startup can safely convert every active row to pending.
	if err := s.queries.RecoverAllocationEntries(ctx, s.tenantID); err != nil {
		return ErrAllocationAccounting
	}
	s.allocationsReady = true
	return nil
}

func (s *Service) retryFailedSettlements(ctx context.Context) error {
	var firstErr error
	var remaining []*observation
	for _, failed := range s.failedSettlements {
		if err := s.persistObservation(ctx, failed); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			remaining = append(remaining, failed)
		}
	}
	s.failedSettlements = remaining
	return firstErr
}
