package groups

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/storage/db"
)

var ErrSchedulingConflict = errors.New("scheduling_policy_conflict")

type SchedulingPolicy struct {
	Enabled           bool    `json:"enabled"`
	MaxMultiplier     float64 `json:"max_multiplier"`
	HorizonFraction   float64 `json:"horizon_fraction"`
	MaxHorizonSeconds int64   `json:"max_horizon_seconds"`
	CurvePower        float64 `json:"curve_power"`
	Revision          int64   `json:"revision"`
}
type SchedulingRevision struct {
	SchedulingPolicy
	CreatedAt int64 `json:"created_at"`
}

func DefaultSchedulingPolicy() SchedulingPolicy {
	return SchedulingPolicy{MaxMultiplier: 2, HorizonFraction: .25, MaxHorizonSeconds: 86400, CurvePower: 2}
}
func (p SchedulingPolicy) Valid() bool {
	return !math.IsNaN(p.MaxMultiplier) && !math.IsNaN(p.HorizonFraction) && !math.IsNaN(p.CurvePower) &&
		p.MaxMultiplier >= 1 && p.MaxMultiplier <= 2 && p.HorizonFraction >= .01 && p.HorizonFraction <= 1 &&
		p.MaxHorizonSeconds >= 60 && p.MaxHorizonSeconds <= 7*86400 && p.CurvePower >= 1 && p.CurvePower <= 4 && p.Revision >= 0
}
func ReadSchedulingPolicy(ctx context.Context, q *db.Queries, tenantID int64) (SchedulingPolicy, error) {
	row, err := q.GetSchedulingPolicy(ctx, tenantID)
	if err != nil {
		return SchedulingPolicy{}, err
	}
	p := DefaultSchedulingPolicy()
	if err = json.Unmarshal([]byte(row.SchedulingPolicy), &p); err != nil {
		return p, err
	}
	p.Revision = row.SchedulingRevision
	if !p.Valid() {
		return p, ErrInput
	}
	return p, nil
}
func (s *Service) SchedulingPolicy(ctx context.Context) (SchedulingPolicy, error) {
	return ReadSchedulingPolicy(ctx, s.queries, s.tenantID)
}
func (s *Service) SaveSchedulingPolicy(ctx context.Context, p SchedulingPolicy) (SchedulingPolicy, error) {
	if !p.Valid() {
		return p, ErrInput
	}
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	raw, err := json.Marshal(p)
	if err != nil {
		return p, ErrInput
	}
	n, err := q.SaveSchedulingPolicy(ctx, db.SaveSchedulingPolicyParams{TenantID: s.tenantID, Policy: string(raw), Revision: p.Revision})
	if err != nil {
		return p, err
	}
	if n == 0 {
		return p, ErrSchedulingConflict
	}
	p.Revision++
	raw, err = json.Marshal(p)
	if err != nil {
		return p, err
	}
	if err = q.InsertSchedulingRevision(ctx, db.InsertSchedulingRevisionParams{TenantID: s.tenantID, Revision: p.Revision, Policy: string(raw), CreatedAt: time.Now().Unix()}); err != nil {
		return p, err
	}
	if err = audit.Record(ctx, q, "settings.update", "settings", "scheduling"); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (s *Service) SchedulingRevisions(ctx context.Context) ([]SchedulingRevision, error) {
	rows, err := s.queries.ListSchedulingRevisions(ctx, s.tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]SchedulingRevision, 0, len(rows))
	for _, r := range rows {
		var p SchedulingPolicy
		if err = json.Unmarshal([]byte(r.Policy), &p); err != nil {
			return nil, err
		}
		p.Revision = r.Revision
		result = append(result, SchedulingRevision{p, r.CreatedAt})
	}
	return result, nil
}
