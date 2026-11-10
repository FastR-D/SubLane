package gateway

import (
	"context"
	"encoding/json"

	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

// Response metadata may refresh a known window, but cannot establish a reset or
// erase an omitted window. Polling remains the authority for window transitions.
func (s *Service) observeUsage(ctx context.Context, id string, revision int64, value upstream.Usage) error {
	c := s.usage
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	e, err := s.loadUsage(ctx, id, c.now())
	if err != nil {
		return err
	}
	old := e.snapshot
	if e.revision != revision || old == nil || value.ReadStartedAt <= old.ReadStartedAt || value.UpdatedAt < old.UpdatedAt || len(value.Limits) != 1 || len(old.Limits) != 1 {
		return nil
	}
	current, next := old.Limits[0], value.Limits[0]
	if current.Name != "" || next.Name != "" || len(current.Windows) != len(next.Windows) || len(next.Windows) == 0 {
		return nil
	}
	for _, w := range current.Windows {
		matched := false
		for _, n := range next.Windows {
			if w.Kind == n.Kind && w.ResetAt != nil && n.ResetAt != nil && *w.ResetAt == *n.ResetAt && *n.ResetAt > value.UpdatedAt && w.UsedPercent != nil && n.UsedPercent != nil && *n.UsedPercent >= *w.UsedPercent {
				matched = true
			}
		}
		if !matched {
			return nil
		}
	}
	if value.Limits[0].Allowed == nil {
		value.Limits[0].Allowed = current.Allowed
	}
	if value.Limits[0].LimitReached == nil {
		value.Limits[0].LimitReached = current.LimitReached
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	n, err := s.queries.SaveAccountUsage(ctx, db.SaveAccountUsageParams{AccountID: id, Revision: revision, Snapshot: raw, UpdatedAt: value.UpdatedAt})
	if err != nil || n == 0 {
		return err
	}
	var saved upstream.Usage
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	e.snapshot, e.err = &saved, nil
	s.recordSchedulingQuota(id, revision, saved)
	// Keep the existing polling cooldown: a response is not proof that its own
	// request has been charged, and must not indefinitely postpone reconciliation.
	return nil
}
