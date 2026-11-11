package gateway

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/content"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/logging"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

type requestKey struct{}
type requestIdentity struct {
	RequestID string
	KeyID     int64
	Transport string
}

func WithRequestIdentity(ctx context.Context, keyID int64, transport string) context.Context {
	if transport != "websocket" {
		transport = "http"
	}
	identity, _ := ctx.Value(requestKey{}).(requestIdentity)
	// Native routes reserve an ID before authentication. Only that pending identity may be reused.
	if identity.RequestID == "" || identity.KeyID != 0 || identity.Transport != transport {
		identity.RequestID = "req_" + rand.Text()
	}
	identity.KeyID, identity.Transport = keyID, transport
	return context.WithValue(ctx, requestKey{}, identity)
}

var safeRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func RequestID(ctx context.Context) string {
	identity, _ := ctx.Value(requestKey{}).(requestIdentity)
	return identity.RequestID
}

var safeModel = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/()+-]{0,159}$`)

// RecordRejection retains authenticated ingress failures without leasing or charging an upstream account.
func (s *Service) RecordRejection(ctx context.Context, userID, groupID int64, kind Kind, code string, started time.Time) {
	// A transport read error can cancel the request before its diagnostic is saved.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	entry, err := s.begin(ctx, userID, groupID, kind)
	if err != nil {
		logging.From(ctx).ErrorContext(ctx, "Unable to persist request metadata", "request_id", RequestID(ctx), "tenant_id", s.tenantID, "error_code", "persist_request_failed")
		return
	}
	entry.started = started
	entry.record.StartedAt = started.Unix()
	entry.finish("rejected", code, "", "")
}

func requestReasoningEffort(input map[string]json.RawMessage, kind Kind) string {
	var effort string
	switch kind {
	case Responses, Compact:
		var reasoning struct {
			Effort string `json:"effort"`
		}
		if json.Unmarshal(input["reasoning"], &reasoning) != nil {
			return ""
		}
		effort = reasoning.Effort
	case Chat:
		if json.Unmarshal(input["reasoning_effort"], &effort) != nil {
			return ""
		}
	}
	// History keeps only known settings, never arbitrary request text or an inferred default.
	switch effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return effort
	default:
		return ""
	}
}

type observation struct {
	// imageInputTokens is the image share of input tokens reported by Images responses; it only
	// affects allowance settlement and is not stored in request history.
	imageInputTokens   int64
	route              *RouteExplanation
	kind               Kind
	service            *Service
	ctx                context.Context
	cancel             context.CancelFunc
	stopParent         func() bool
	once               sync.Once
	started            time.Time
	record             db.RecordRequestParams
	revision           int64
	leased             bool
	memberLeased       bool
	schemeID           int64
	allocationTracked  bool
	upstreamDispatched bool
	usageFinal         bool
	sequence           int64
	quotaReadStartedAt int64
	suppressModel      bool
	quotaRevision      int64
	quota              *upstream.Usage
	modelState         *modelRuntime
	modelLimited       bool
	modelTransient     bool
}

func (s *Service) begin(ctx context.Context, userID, groupID int64, kind Kind) (*observation, error) {
	if _, err := s.queries.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: groupID, TenantID: s.tenantID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, groups.ErrUnavailable
		}
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	s.workers.Add(1)
	operation, cancel := context.WithCancel(ctx)
	route := &RouteExplanation{}
	operation = context.WithValue(operation, routeKey{}, route)
	identity, _ := ctx.Value(requestKey{}).(requestIdentity)
	if identity.RequestID == "" {
		identity.RequestID = "req_" + rand.Text()
	}
	if identity.Transport == "" {
		identity.Transport = "http"
	}
	name := "responses"
	if kind == Chat {
		name = "chat"
	} else if kind == Compact {
		name = "compact"
	} else if kind.IsImage() {
		name = "images"
	} else if kind == Messages {
		name = "messages"
	} else if kind.IsGemini() {
		name = "gemini"
	}
	started := s.now()
	return &observation{route: route, kind: kind, service: s, ctx: operation, cancel: cancel, stopParent: context.AfterFunc(s.runContext, cancel), started: started, record: db.RecordRequestParams{RequestID: identity.RequestID, UserID: userID, GroupID: groupID, KeyID: identity.KeyID, Transport: identity.Transport, Operation: name, StartedAt: started.Unix()}}, nil
}
func (e *observation) finish(outcome, code, penalty, retry string) {
	e.once.Do(func() {
		defer e.service.workers.Done()
		defer e.cancel()
		defer e.stopParent()
		e.record.Outcome = outcome
		e.record.ErrorCode = code
		e.record.DurationMs = max(0, e.service.now().Sub(e.started).Milliseconds())
		var quotaFailed, runtimeFailed, historyFailed bool
		// Emit after the later mutex-unlock defer: slow stdout must not block admission.
		defer func() {
			if e.service.telemetry != nil {
				e.service.telemetry.Observe(outcome, e.record.DurationMs, e.record.FirstTokenMs)
			}
			if quotaFailed {
				e.log(slog.LevelWarn, "Unable to persist response quota", "persist_quota_failed")
			}
			if runtimeFailed {
				e.log(slog.LevelError, "Unable to persist account cooldown", "persist_cooldown_failed")
			}
			if historyFailed {
				e.log(slog.LevelError, "Unable to persist request metadata", "persist_request_failed")
			}
			level := slog.LevelDebug
			switch outcome {
			case "success":
				if e.record.Transport == "websocket" {
					level = slog.LevelInfo
				}
			case "canceled":
			case "incomplete", "rejected":
				level = slog.LevelWarn
			default:
				level = slog.LevelError
			}
			if code == "rate_limited" {
				level = slog.LevelWarn
			}
			e.log(level, "Gateway request completed", code)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s := e.service
		if e.quota != nil && (outcome == "success" || outcome == "incomplete") {
			if err := s.observeUsage(ctx, e.record.AccountID, e.quotaRevision, *e.quota); err != nil {
				quotaFailed = true
			}
		}
		s.mu.Lock()
		// A request deadline or unknown route failure is not evidence that every
		// model on this account is unavailable. Explicit responses have model scope.
		e.modelTransient = s.policy.TransientThreshold > 0 && penalty == "upstream_error" && (code == "stream_interrupted" || e.record.UpstreamStatus != nil && *e.record.UpstreamStatus >= 500)
		if e.memberLeased {
			if s.memberActive[e.record.UserID] <= 1 {
				delete(s.memberActive, e.record.UserID)
			} else {
				s.memberActive[e.record.UserID]--
			}
		}
		if e.leased {
			if state := s.health[e.record.AccountID]; state != nil {
				changed := false
				if state.revision == e.revision {
					if penalty == "rate_limited" {
						state.Failures = min(16, state.Failures+1)
						state.lastFailureAt = s.now().UnixMilli()
						s.sequence++
						state.lastFailureSequence = s.sequence
						state.Reason = penalty
						changed = true
						delay := retryDelay(retry, s.now(), s.policy.RateLimitCooldown)
						if delay > 0 {
							state.CooldownUntil = max(state.CooldownUntil, s.now().Unix()+delay)
						}
						// Sequence order, rather than wall-clock milliseconds, protects against late successes in the same tick.
					} else if (outcome == "success" || outcome == "incomplete") && state.lastFailureSequence < e.sequence && state.CooldownUntil <= s.now().Unix() {
						changed = state.Failures != 0 || state.CooldownUntil != 0
						state.Failures = 0
						state.CooldownUntil = 0
						state.Reason = ""
						state.lastFailureAt = 0
						state.lastFailureSequence = 0
					}
					if changed {
						if err := s.persistRuntime(ctx, state); err != nil {
							runtimeFailed = true
						}
					}
				}
				state.InFlight = max(0, state.InFlight-1)
			}
		}
		// Admission must not observe released leases before the token charge commits.
		if s.finishModelRuntime(ctx, e, outcome, retry) {
			runtimeFailed = true
		}
		defer s.mu.Unlock()
		err := s.persistObservation(ctx, e)
		if err != nil {
			if e.allocationTracked {
				s.failedSettlements = append(s.failedSettlements, e)
			}
			historyFailed = true
		}
		if e.leased {
			s.notifyCapacity(e.record.AccountID)
		}
	})
}

func (s *Service) persistObservation(ctx context.Context, e *observation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if e.allocationTracked {
		// A previous commit may have succeeded even if its caller saw an error.
		recorded, err := q.RequestRecorded(ctx, e.record.RequestID)
		if err != nil {
			return err
		}
		if recorded {
			return tx.Commit()
		}
	}
	if err := e.settleAllocation(ctx, q); err != nil {
		return err
	}
	record := e.record
	record.RouteExplanation = encodeRoute(e.route)
	// Keep the execution model for allowance settlement, but never persist a matched model value.
	if e.suppressModel {
		record.Model = ""
	}
	if err := q.RecordRequest(ctx, record); err != nil {
		return err
	}
	if err := recordStatistics(ctx, q, record); err != nil {
		return err
	}
	return tx.Commit()
}
func classify(ctx context.Context, err error) (outcome, code, penalty string) {
	if errors.Is(err, content.ErrBlocked) || errors.Is(err, content.ErrUnavailable) {
		return "rejected", err.Error(), ""
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "canceled", "client_disconnected", ""
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "error", "timeout", "timeout"
	}
	var rejected *upstream.UpstreamError
	if errors.As(err, &rejected) {
		return statusOutcome(rejected.Status)
	}
	switch {
	case errors.Is(err, allocations.ErrQuota), errors.Is(err, allocations.ErrPending), errors.Is(err, allocations.ErrRisk), errors.Is(err, allocations.ErrUnavailable), errors.Is(err, allocations.ErrUnpriced):
		return "rejected", err.Error(), ""
	case errors.Is(err, ErrMemberBusy):
		return "rejected", "member_busy", ""
	case errors.Is(err, ErrMemberRate):
		return "rejected", "member_rate_limited", ""
	case errors.Is(err, ErrAllocationAccounting):
		return "error", "allocation_accounting_unavailable", ""
	case errors.Is(err, ErrQuotaExhausted):
		return "rejected", "quota_exhausted", ""
	case errors.Is(err, ErrAccountQueueFull), errors.Is(err, ErrAccountWaitTimeout):
		return "rejected", err.Error(), ""
	case errors.Is(err, ErrAccountBusy):
		return "rejected", "account_busy", ""
	case errors.Is(err, ErrAccountCooling):
		return "rejected", "account_cooling", ""
	case errors.Is(err, ErrModelCooling):
		return "rejected", "model_cooling", ""
	case errors.Is(err, ErrModelConflict):
		return "rejected", "conversation_model_conflict", ""
	case errors.Is(err, ErrNoAccount), errors.Is(err, ErrAffinityUnavailable), errors.Is(err, accounts.ErrDisabled), errors.Is(err, accounts.ErrNotFound):
		return "rejected", "account_unavailable", ""
	case errors.Is(err, groups.ErrUnavailable):
		return "rejected", "group_unavailable", ""
	case errors.Is(err, accounts.ErrReauthorize):
		return "error", "auth_required", ""
	case errors.Is(err, ErrModelUnavailable):
		return "rejected", "model_not_available", ""
	case errors.Is(err, ErrCatalogUnavailable):
		return "rejected", "model_catalog_unavailable", ""
	case errors.Is(err, ErrModelNotAllowed):
		return "rejected", "model_not_allowed", ""
	case errors.Is(err, upstream.ErrInput), errors.Is(err, upstream.ErrContinuation):
		return "rejected", "invalid_request", ""
	case errors.Is(err, upstream.ErrInterrupted), errors.Is(err, upstream.ErrResponse):
		return "error", "stream_interrupted", "upstream_error"
	case errors.Is(err, accounts.ErrRefresh):
		// Credential retries have their own backoff; polling it must not extend model cooldowns.
		return "error", "refresh_failed", ""
	case errors.Is(err, upstream.ErrUpstream):
		return "error", "upstream_unavailable", "upstream_error"
	default:
		return "error", "request_failed", ""
	}
}
func (e *observation) fail(err error) {
	var rejected *upstream.UpstreamError
	if errors.As(err, &rejected) {
		status := int64(rejected.Status)
		e.record.UpstreamStatus = &status
		outcome, code, penalty := statusOutcome(rejected.Status)
		if rejected.Status == 429 && e.modelState != nil && rejected.LimitedModel == e.modelState.key.model {
			e.modelLimited = true
			code, penalty = "model_rate_limited", ""
		}
		e.finish(outcome, code, penalty, rejected.RetryAfter)
		return
	}
	outcome, code, penalty := classify(e.ctx, err)
	e.finish(outcome, code, penalty, "")
}

func (e *observation) observe(raw []byte) {
	type usage struct {
		Input   *int64 `json:"input_tokens"`
		Output  *int64 `json:"output_tokens"`
		Details struct {
			Cached *int64 `json:"cached_tokens"`
			Image  *int64 `json:"image_tokens"`
		} `json:"input_tokens_details"`
	}
	var value struct {
		Response struct {
			Usage *usage `json:"usage"`
		} `json:"response"`
		Usage *usage `json:"usage"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	tokens := value.Response.Usage
	if tokens == nil {
		tokens = value.Usage
	}
	if tokens == nil {
		return
	}
	valid := func(n *int64) *int64 {
		if n == nil || *n < 0 || *n > 1_000_000_000 {
			return nil
		}
		return n
	}
	e.record.InputTokens = valid(tokens.Input)
	e.record.OutputTokens = valid(tokens.Output)
	e.record.CachedTokens = valid(tokens.Details.Cached)
	e.imageInputTokens = 0
	// Settlement rejects overlapping categories, so an inconsistent split keeps the text input rate.
	if image := valid(tokens.Details.Image); image != nil && e.record.InputTokens != nil {
		cached := int64(0)
		if e.record.CachedTokens != nil {
			cached = *e.record.CachedTokens
		}
		if cached+*image <= *e.record.InputTokens {
			e.imageInputTokens = *image
		}
	}
}
