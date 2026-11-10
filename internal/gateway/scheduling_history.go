package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

const maxSchedulingPending = 4096

type SchedulingSample struct {
	Kind          string   `json:"kind"`
	AccountID     string   `json:"account_id"`
	GroupID       int64    `json:"group_id"`
	Bucket        int64    `json:"bucket"`
	Scope         string   `json:"scope"`
	Revision      int64    `json:"revision"`
	ObservedAt    int64    `json:"observed_at"`
	Samples       int64    `json:"samples"`
	InFlightTotal int64    `json:"in_flight_total"`
	Peak          int64    `json:"peak"`
	Limit         int64    `json:"limit"`
	Expected      float64  `json:"expected"`
	Selected      int64    `json:"selected"`
	Decisions     int64    `json:"decisions"`
	Weight        float64  `json:"weight"`
	ResetFactor   float64  `json:"reset_factor"`
	Remaining     *float64 `json:"remaining"`
	ResetAt       *int64   `json:"reset_at"`
	WindowSeconds *int64   `json:"window_seconds"`
}

func (v SchedulingSample) key() string {
	return fmt.Sprintf("%s:%s:%d:%d:%s:%d", v.Kind, v.AccountID, v.GroupID, v.Bucket, v.storageScope(), v.Revision)
}
func (v SchedulingSample) storageScope() string {
	if v.Kind == "quota" && v.ResetAt != nil {
		return fmt.Sprintf("%s:%d", v.Scope, *v.ResetAt)
	}
	return v.Scope
}
func mergeScheduling(a, b SchedulingSample) SchedulingSample {
	if a.Kind == "" {
		return b
	}
	if a.ObservedAt > b.ObservedAt {
		a, b = b, a
	}
	b.Samples += a.Samples
	b.InFlightTotal += a.InFlightTotal
	b.Peak = max(b.Peak, a.Peak)
	b.Expected += a.Expected
	b.Selected += a.Selected
	b.Decisions += a.Decisions
	return b
}

type schedulingCollector struct {
	mu      sync.Mutex
	pending map[string]SchedulingSample
	dropped int64
	started bool
}

func (c *schedulingCollector) add(v SchedulingSample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = make(map[string]SchedulingSample)
	}
	k := v.key()
	old, ok := c.pending[k]
	if !ok && len(c.pending) >= maxSchedulingPending {
		c.dropped++
		return
	}
	c.pending[k] = mergeScheduling(old, v)
}
func (c *schedulingCollector) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

func (s *Service) recordSchedulingPlacement(groupID int64, scope string, revision int64, selected string, weights []SchedulingWeight) {
	now := s.now().Unix()
	for _, w := range weights {
		v := SchedulingSample{Kind: "placement", AccountID: w.AccountID, GroupID: groupID, Bucket: now / 3600 * 3600, ObservedAt: now, Scope: scope, Revision: revision, Expected: w.TargetShare, Decisions: 1, Weight: w.EffectiveWeight, ResetFactor: w.Reset.Factor}
		if selected == w.AccountID {
			v.Selected = 1
		}
		s.scheduling.add(v)
	}
}
func (s *Service) recordSchedulingQuota(id string, revision int64, value upstream.Usage) {
	count := 0
	for _, limit := range value.Limits {
		if limit.Hidden || len(limit.Name) > 128 {
			continue
		}
		for _, w := range limit.Windows {
			if count >= 64 {
				return
			}
			count++
			if w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || *w.UsedPercent > 100 || len(w.Kind) > 32 {
				continue
			}
			remaining := 100 - *w.UsedPercent
			s.scheduling.add(SchedulingSample{Kind: "quota", AccountID: id, Bucket: value.UpdatedAt / 300 * 300, ObservedAt: value.UpdatedAt, Scope: limit.Name + ":" + w.Kind, Revision: revision, Remaining: &remaining, ResetAt: w.ResetAt, WindowSeconds: w.WindowSeconds})
		}
	}
}

// StartScheduling collects local pressure independently of open administrator pages.
// The process context owns the worker; Close joins it before storage closes.
func (s *Service) StartScheduling() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.scheduling.started {
		return
	}
	s.scheduling.started = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(s.runContext, 5*time.Second)
			_ = s.collectScheduling(ctx)
			cancel()
			select {
			case <-s.runContext.Done():
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = s.flushScheduling(ctx)
				cancel()
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *Service) collectScheduling(ctx context.Context) error {
	s.mu.Lock()
	if err := s.loadRuntime(ctx); err != nil {
		s.mu.Unlock()
		return err
	}
	now := s.now().Unix()
	for id, v := range s.health {
		s.scheduling.add(SchedulingSample{Kind: "pressure", AccountID: id, Bucket: now / 60 * 60, ObservedAt: now, Samples: 1, InFlightTotal: v.InFlight, Peak: v.InFlight, Limit: v.MaxConcurrency})
	}
	s.mu.Unlock()
	return s.flushScheduling(ctx)
}
func (s *Service) flushScheduling(ctx context.Context) (err error) {
	c := &s.scheduling
	c.mu.Lock()
	batch := make(map[string]SchedulingSample)
	for k, v := range c.pending {
		if len(batch) >= 500 {
			break
		}
		batch[k] = v
		delete(c.pending, k)
	}
	dropped := c.dropped
	c.dropped = 0
	c.mu.Unlock()
	defer func() {
		if err != nil {
			for _, v := range batch {
				c.add(v)
			}
			c.mu.Lock()
			c.dropped += dropped
			c.mu.Unlock()
		}
	}()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	for _, v := range batch {
		raw, e := q.GetSchedulingSample(ctx, db.GetSchedulingSampleParams{TenantID: s.tenantID, AccountID: v.AccountID, GroupID: v.GroupID, Bucket: v.Bucket, Kind: v.Kind, Scope: v.storageScope(), Revision: v.Revision})
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			var old SchedulingSample
			if e = json.Unmarshal([]byte(raw), &old); e != nil {
				return e
			}
			v = mergeScheduling(old, v)
		}
		body, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if e = q.SaveSchedulingSample(ctx, db.SaveSchedulingSampleParams{TenantID: s.tenantID, AccountID: v.AccountID, GroupID: v.GroupID, Bucket: v.Bucket, Kind: v.Kind, Scope: v.storageScope(), Revision: v.Revision, ObservedAt: v.ObservedAt, Payload: string(body)}); e != nil {
			return e
		}
	}
	if err = q.MarkSchedulingCollection(ctx, db.MarkSchedulingCollectionParams{TenantID: s.tenantID, Now: s.now().Unix(), Dropped: dropped}); err != nil {
		return err
	}
	return tx.Commit()
}

type SchedulingUsage struct {
	AccountID           string `json:"account_id"`
	Hour                int64  `json:"hour"`
	Requests            int64  `json:"requests"`
	Completed           int64  `json:"completed"`
	Errors              int64  `json:"errors"`
	Limited             int64  `json:"limited"`
	DurationMS          int64  `json:"duration_ms"`
	WaitMS              int64  `json:"wait_ms"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	InputReported       int64  `json:"input_reported"`
	OutputReported      int64  `json:"output_reported"`
	FirstOutputMS       int64  `json:"first_output_ms"`
	FirstOutputReported int64  `json:"first_output_reported"`
}
type SchedulingHistory struct {
	AsOf           int64              `json:"as_of"`
	CollectedSince int64              `json:"collected_since"`
	LastFlush      int64              `json:"last_flush"`
	Dropped        int64              `json:"dropped"`
	Truncated      bool               `json:"truncated"`
	Samples        []SchedulingSample `json:"samples"`
	Usage          []SchedulingUsage  `json:"usage"`
}

func (s *Service) SchedulingHistory(ctx context.Context, userID, groupID, hours int64, accountID string) (SchedulingHistory, error) {
	result := SchedulingHistory{AsOf: s.now().Unix(), Samples: []SchedulingSample{}, Usage: []SchedulingUsage{}}
	if hours < 1 || hours > 90*24 || len(accountID) > 32 || strings.ContainsAny(accountID, "\r\n") {
		return result, upstream.ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if _, err = poolAccounts(ctx, q, userID, groupID); err != nil {
		return result, err
	}
	coverage, err := q.GetSchedulingCoverage(ctx, s.tenantID)
	if err != nil {
		return result, err
	}
	result.CollectedSince = coverage.SchedulingStartedAt
	result.LastFlush = coverage.SchedulingLastFlush
	result.Dropped = coverage.SchedulingDropped
	since := (result.AsOf - hours*3600) / 3600 * 3600
	rows, err := q.ListSchedulingSamples(ctx, db.ListSchedulingSamplesParams{TenantID: s.tenantID, GroupID: groupID, Since: since, AccountID: accountID})
	if err != nil {
		return result, err
	}
	if len(rows) > 2000 {
		result.Truncated = true
		rows = rows[:2000]
	}
	for _, raw := range rows {
		var v SchedulingSample
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return result, err
		}
		result.Samples = append(result.Samples, v)
	}
	usage, err := q.ListSchedulingUsage(ctx, db.ListSchedulingUsageParams{TenantID: s.tenantID, GroupID: groupID, Since: since, AccountID: accountID})
	if err != nil {
		return result, err
	}
	if len(usage) > 2000 {
		result.Truncated = true
		usage = usage[:2000]
	}
	for _, v := range usage {
		result.Usage = append(result.Usage, SchedulingUsage{v.AccountID, v.Hour, v.Requests, v.Completed, v.Errors, v.Limited, v.DurationMs, v.WaitMs, v.InputTokens, v.OutputTokens, v.InputReported, v.OutputReported, v.FirstOutputMs, v.FirstOutputReported})
	}
	return result, tx.Commit()
}

func (s *Service) recordSchedulingUsage(ctx context.Context, q *db.Queries, e *observation) error {
	r := e.record
	if r.AccountID == "" || !e.leased {
		return nil
	}
	v := db.RecordSchedulingUsageParams{TenantID: s.tenantID, GroupID: r.GroupID, AccountID: r.AccountID, Hour: r.StartedAt / 3600 * 3600, DurationMs: r.DurationMs, WaitMs: e.waitMS}
	if r.Outcome == "success" || r.Outcome == "incomplete" {
		v.Completed = 1
	}
	if r.Outcome == "error" {
		v.Errors = 1
	}
	if r.ErrorCode == "rate_limited" || r.ErrorCode == "model_rate_limited" {
		v.Limited = 1
	}
	if r.InputTokens != nil {
		v.InputTokens = *r.InputTokens
		v.InputReported = 1
	}
	if r.OutputTokens != nil {
		v.OutputTokens = *r.OutputTokens
		v.OutputReported = 1
	}
	if r.FirstTokenMs != nil {
		v.FirstOutputMs = *r.FirstTokenMs
		v.FirstOutputReported = 1
	}
	return q.RecordSchedulingUsage(ctx, v)
}
