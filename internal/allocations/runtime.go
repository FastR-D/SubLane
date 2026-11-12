package allocations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/storage/db"
)

func unix(n int64) time.Time { return time.Unix(n, 0) }

// A ratio rule stores percentages while entries use the selected accounting unit.
func entryMode(c Config) string {
	if c.Mode == "ratio" {
		return c.RatioUnit
	}
	if c.Mode == "windows" {
		return "amount"
	}
	return c.Mode
}
func effectiveWindow(rev Revision, now int64, location *time.Location) (int64, int64) {
	windows := effectiveWindows(rev, now, location)
	longest := windows[0]
	for _, window := range windows[1:] {
		if window.end > longest.end {
			longest = window
		}
	}
	// Retention follows the latest active window, so short-window cleanup cannot erase longer-window usage.
	return longest.start, longest.end
}
func Authorize(ctx context.Context, q *db.Queries, scheme, user, group, now int64) (Revision, error) {
	row, err := q.GetAllocationScheme(ctx, scheme)
	if err != nil || row.GroupID != group {
		return Revision{}, ErrUnavailable
	}
	allowed, err := q.CanUseAllocation(ctx, db.CanUseAllocationParams{ID: scheme, UserID: user})
	if err != nil {
		return Revision{}, err
	}
	if !allowed {
		return Revision{}, ErrUnavailable
	}
	rev, err := Current(ctx, q, scheme, now)
	if err != nil {
		return rev, err
	}
	for _, m := range rev.Config.Members {
		if m.UserID == user {
			return rev, nil
		}
	}
	return rev, ErrUnavailable
}
func KeyScheme(ctx context.Context, q *db.Queries, key, user, group, now int64) (int64, error) {
	id, err := q.GetKeyAllocation(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = q.GetPoolAllocation(ctx, group); err == nil {
			return 0, ErrUnavailable
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	_, err = Authorize(ctx, q, id, user, group, now)
	return id, err
}
func rateFor(rev Revision, model string) (Rate, error) {
	for _, r := range rev.Config.Rates {
		if r.Model == model {
			return r, nil
		}
	}
	return Rate{}, ErrUnpriced
}

const maxOutstandingRiskRequests = 4

type riskBudget struct {
	reserved int64
	room     int64
	limited  bool
}

type memberWindowUsage struct {
	window    allocationWindow
	limit     int64
	unlimited bool
	used      int64
	tokens    int64
	active    int64
	pending   int64
	risk      riskBudget
}

func (a *accounting) member(ctx context.Context, member Share, config Config, window allocationWindow) (memberWindowUsage, error) {
	mode := entryMode(config)
	usage, err := a.q.AllocationMemberUsage(ctx, db.AllocationMemberUsageParams{SchemeID: a.scheme, UserID: member.UserID, StartedFrom: window.start, StartedTo: window.end, Mode: mode})
	if err != nil {
		return memberWindowUsage{}, err
	}
	exposure, err := a.q.AllocationMemberExposure(ctx, db.AllocationMemberExposureParams{SchemeID: a.scheme, UserID: member.UserID, StartedFrom: window.start, StartedTo: window.end, Mode: mode})
	if err != nil {
		return memberWindowUsage{}, err
	}
	limit := memberLimit(config, member, window)
	unlimited := config.Mode == "windows" && limit == 0
	risk := riskBudget{}
	if !unlimited {
		active, held, err := a.reservations(ctx, member.UserID, member, config, window)
		if err != nil {
			return memberWindowUsage{}, err
		}
		perRequest := reservation(limit)
		// The buffer is shared headroom; another request still needs its full reservation.
		room := max(0, limit+perRequest-usage.Used-held)
		risk = riskBudget{reserved: held, room: room, limited: active >= maxOutstandingRiskRequests || room < perRequest}
	}
	return memberWindowUsage{
		window: window, limit: limit, unlimited: unlimited, used: usage.Used, tokens: usage.Tokens,
		active: exposure.Active, pending: exposure.Pending,
		risk: risk,
	}, nil
}

func Check(ctx context.Context, q *db.Queries, scheme, user, group int64, account, model string, now int64, location *time.Location) (Revision, error) {
	rev, err := Authorize(ctx, q, scheme, user, group, now)
	if err != nil {
		return rev, err
	}
	if entryMode(rev.Config) == "amount" {
		if _, err = rateFor(rev, model); err != nil {
			return rev, err
		}
	}
	windows := effectiveWindows(rev, now, location)
	accounting := newAccounting(q, scheme, rev, windows)
	for _, m := range rev.Config.Members {
		if m.UserID != user {
			continue
		}
		riskLimited := false
		for _, window := range windows {
			state, err := accounting.member(ctx, m, rev.Config, window)
			if err != nil {
				return rev, err
			}
			if !state.unlimited && state.used >= state.limit {
				return rev, ErrQuota
			}
			if rev.Config.Mode == "ratio" {
				group, err := accounting.group(ctx, window)
				if err != nil {
					return rev, err
				}
				if group.used >= window.total(rev.Config) {
					return rev, ErrQuota
				}
				riskLimited = riskLimited || group.room < reservation(state.limit)
			}
			riskLimited = riskLimited || state.risk.limited
		}
		if riskLimited {
			return rev, ErrRisk
		}
	}
	return rev, nil
}

type Request struct {
	ID                        string
	SchemeID, UserID, GroupID int64
	AccountID, Model          string
	StartedAt                 int64
}

func Begin(ctx context.Context, q *db.Queries, r Request, now int64, location *time.Location) error {
	rev, err := Check(ctx, q, r.SchemeID, r.UserID, r.GroupID, r.AccountID, r.Model, now, location)
	if err != nil {
		return err
	}
	start, end := effectiveWindow(rev, now, location)
	if err = q.BeginAllocationEntry(ctx, db.BeginAllocationEntryParams{RequestID: r.ID, SchemeID: r.SchemeID, RevisionID: rev.ID, UserID: r.UserID, AccountID: r.AccountID, Model: r.Model, Mode: entryMode(rev.Config), WindowStart: start, ResetAt: end, StartedAt: r.StartedAt}); err != nil {
		return err
	}
	return nil
}

type Completion struct {
	Input, Output, Cached int64
	// ImageInput is the image share of Input, reported by image generation responses.
	ImageInput int64
	Known      bool
	Dispatched bool
	Rejected   bool
}

func Finish(ctx context.Context, q *db.Queries, id string, c Completion, now int64, manual bool) error {
	e, err := q.GetAllocationEntry(ctx, id)
	if err != nil {
		return err
	}
	if !manual && e.State != "active" {
		return nil
	}
	if manual {
		// Only an identical breakdown is a retry; a different image split would have charged differently.
		if e.State == "settled" && e.Manual == 1 && e.InputTokens == c.Input && e.OutputTokens == c.Output && e.CachedTokens == c.Cached && e.ImageInputTokens == c.ImageInput {
			return nil
		}
		if e.State != "pending" || c.Input < e.InputTokens || c.Output < e.OutputTokens {
			return ErrSettlement
		}
	}
	state := "settled"
	if c.Dispatched && !c.Rejected && !c.Known {
		state = "pending"
	}
	cost := c.Input + c.Output
	if c.Input < 0 || c.Output < 0 || c.Cached < 0 || c.Cached > c.Input || c.Input > 1_000_000_000 || c.Output > 1_000_000_000 {
		return ErrInput
	}
	if e.Mode != "tokens" {
		row, err := q.GetAllocationRevision(ctx, e.RevisionID)
		if err != nil {
			return err
		}
		rev, err := storedRevision(row)
		if err != nil {
			return err
		}
		rate, err := rateFor(rev, e.Model)
		if err != nil {
			return err
		}
		cost, err = CostWithImages(rate, c.Input, c.ImageInput, c.Output, c.Cached)
		if err != nil {
			return err
		}
	}
	if state == "pending" {
		// Partial observations remain available for correction but are not a charge.
		cost = 0
	}
	return q.FinishAllocationEntry(ctx, db.FinishAllocationEntryParams{RequestID: id, FinishedAt: now, State: state, InputTokens: c.Input, OutputTokens: c.Output, CachedTokens: c.Cached, ImageInputTokens: c.ImageInput, Cost: cost, Manual: bit(manual)})
}
func (s *Service) Settle(ctx context.Context, scheme int64, id string, c Completion) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	if _, err := q.GetTenantAllocationScheme(ctx, db.GetTenantAllocationSchemeParams{ID: scheme, TenantID: s.tenantID}); err != nil {
		return ErrNotFound
	}
	e, err := q.GetAllocationEntry(ctx, id)
	if err != nil || e.SchemeID != scheme {
		return ErrSettlement
	}
	c.Known = true
	c.Dispatched = true
	if err = Finish(ctx, q, id, c, s.now().UnixMilli(), true); err != nil {
		return err
	}
	if err = audit.Record(ctx, q, "allocation.settle", "allocation", audit.ID(scheme)); err != nil {
		return err
	}
	return tx.Commit()
}

// Prune only closed accounting periods; unresolved requests remain available.
func Prune(ctx context.Context, q *db.Queries, before int64) (int64, error) {
	return q.PruneAllocations(ctx, before)
}
