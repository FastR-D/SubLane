package gateway

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/content"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/logging"
	"github.com/murongg/SubLane/internal/metrics"
	"github.com/murongg/SubLane/internal/pricing"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/timezone"
	"github.com/murongg/SubLane/internal/upstream"
)

var (
	ErrAffinityUnavailable = errors.New("conversation_account_unavailable")
	ErrModelConflict       = errors.New("conversation_model_conflict")
	ErrNoAccount           = errors.New("no_accounts_available")
	ErrBusy                = errors.New("gateway_busy")
	ErrAffinityLimit       = errors.New("conversation_limit")
)

type Kind uint8

const (
	Responses Kind = iota
	Chat
	Compact
	Messages
	Gemini
	GeminiStream
	Images
	ImageEdits
)

func (k Kind) IsGemini() bool { return k == Gemini || k == GeminiStream }

// IsImage reports OpenAI Images requests, which only Codex subscriptions can serve.
func (k Kind) IsImage() bool { return k == Images || k == ImageEdits }

// codexOnly kinds are served by native Codex endpoints without protocol translation.
func (k Kind) codexOnly() bool { return k == Compact || k.IsImage() }

type Service struct {
	telemetry          *metrics.Collector
	db                 *sql.DB
	queries            *db.Queries
	accounts           *accounts.Service
	tenantID           int64
	provider           *upstream.Client
	slots              chan struct{}
	mu                 sync.Mutex
	next               map[string]int
	health             map[string]*Runtime
	modelHealth        map[modelKey]*modelRuntime
	memberActive       map[int64]int64
	waiting            map[int64]int
	capacityChanged    map[int64]chan struct{}
	accountWaitTimeout time.Duration
	policy             Policy
	now                func() time.Time
	runContext         context.Context
	stopRuntime        context.CancelFunc
	workers            sync.WaitGroup
	closed             bool
	allocationsReady   bool
	failedSettlements  []*observation
	sequence           int64
	usage              *usageCache
	catalog            *catalogCache
	pricing            *pricing.Service
	timeZone           *timezone.Service
	content            *content.Service
}

func (s *Service) SetContent(rules *content.Service) { s.content = rules }

func (s *Service) SetTimeZone(zone *timezone.Service) { s.timeZone = zone }
func (s *Service) location() *time.Location {
	if s.timeZone == nil {
		return time.UTC
	}
	return s.timeZone.Location()
}

func New(ctx context.Context, connection *sql.DB, accounts *accounts.Service, provider *upstream.Client, catalogs ...*pricing.Service) *Service {
	return NewForTenant(ctx, connection, accounts, provider, 1, catalogs...)
}

func NewForTenant(ctx context.Context, connection *sql.DB, accounts *accounts.Service, provider *upstream.Client, tenantID int64, catalogs ...*pricing.Service) *Service {
	return NewForTenantWithPolicy(ctx, connection, accounts, provider, tenantID, DefaultPolicy(), catalogs...)
}

func NewForTenantWithPolicy(ctx context.Context, connection *sql.DB, accounts *accounts.Service, provider *upstream.Client, tenantID int64, policy Policy, catalogs ...*pricing.Service) *Service {
	runContext, stopRuntime := context.WithCancel(ctx)
	var catalog *pricing.Service
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	return &Service{memberActive: make(map[int64]int64), waiting: make(map[int64]int), capacityChanged: make(map[int64]chan struct{}), accountWaitTimeout: policy.QueueTimeout, policy: policy, next: make(map[string]int), health: make(map[string]*Runtime), modelHealth: make(map[modelKey]*modelRuntime), now: time.Now, runContext: runContext, stopRuntime: stopRuntime, db: connection, queries: db.New(connection), accounts: accounts, tenantID: tenantID, provider: provider, slots: make(chan struct{}, policy.Capacity), usage: newUsageCache(ctx), catalog: newCatalogCache(ctx), pricing: catalog}
}

func (s *Service) Acquire() (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	default:
		return nil, ErrBusy
	}
}

func (s *Service) MaxRequestBody() int64 {
	return s.provider.MaxRequestBody()
}

func (s *Service) Open(ctx context.Context, userID, groupID int64, raw []byte, headers http.Header, kind Kind) (exchange *Exchange, failure error) {
	entry, err := s.begin(ctx, userID, groupID, kind)
	if err != nil {
		return nil, err
	}
	ctx = entry.ctx
	defer func() {
		if exchange == nil {
			entry.fail(failure)
		}
	}()
	if int64(len(raw)) > s.MaxRequestBody() {
		return nil, upstream.ErrInput
	}
	if kind == Messages {
		if err := upstream.ValidateMessages(raw, s.MaxRequestBody()); err != nil {
			return nil, err
		}
	}
	if kind.IsGemini() {
		if err := upstream.ValidateGemini(raw, s.MaxRequestBody()); err != nil {
			return nil, err
		}
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil || input == nil {
		return nil, upstream.ErrInput
	}
	var model string
	if json.Unmarshal(input["model"], &model) != nil {
		return nil, upstream.ErrInput
	}
	if !safeModel.MatchString(model) {
		return nil, upstream.ErrInput
	}
	entry.record.Model = model
	entry.record.ReasoningEffort = requestReasoningEffort(input, kind)
	// Inspect before later validation can publish client-supplied model metadata on a failure.
	if err := s.checkContent(ctx, entry, raw); err != nil {
		return nil, err
	}
	requestedModel := model
	// Retry checks inspect client content before routing adds internal session IDs.
	original := raw
	provider, model := groups.SplitModel(model)
	if model == "" || len(model) > 128 {
		return nil, upstream.ErrInput
	}
	entry.record.Provider = provider
	input["model"], _ = json.Marshal(model)
	if kind.codexOnly() && provider != "" && provider != "codex" {
		return nil, upstream.ErrInput
	}
	// Image models only exist behind the Images endpoints, and those endpoints only accept them.
	if kind.IsImage() != upstream.IsImageModel(model) {
		return nil, upstream.ErrInput
	}
	session := headers.Get("Session_id")
	if session == "" {
		session = headers.Get("X-Opencode-Session")
	}
	if session == "" && len(input["prompt_cache_key"]) > 0 {
		if json.Unmarshal(input["prompt_cache_key"], &session) != nil {
			return nil, upstream.ErrInput
		}
	}
	fromMetadata := false
	if kind == Messages && session == "" && len(input["metadata"]) > 0 {
		var metadata struct {
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(input["metadata"], &metadata) != nil {
			return nil, upstream.ErrInput
		}
		session = metadata.UserID
		fromMetadata = session != ""
	}
	if len(session) > 1024 {
		return nil, upstream.ErrInput
	}
	if err := s.AuthorizeModel(ctx, userID, groupID, requestedModel); err != nil {
		return nil, err
	}
	discoveryProvider := provider
	if kind.codexOnly() {
		discoveryProvider = "codex"
	}
	if err := s.warmCatalogs(ctx, userID, groupID, discoveryProvider, true); err != nil {
		return nil, err
	}
	if err := s.warmUsage(ctx, userID, groupID, discoveryProvider); err != nil {
		return nil, err
	}
	affinity := requestAffinity(input, kind)
	if continued, _ := ctx.Value(continuationKey{}).(bool); continued {
		affinity = continuationAffinity
	}
	// Hints are inert unless the group enables stickiness; selection reads that policy in its transaction.
	ctx = withStickyHints(ctx, requestStickyHints(headers, input, kind, session, fromMetadata))
	id, digest, err := s.leaseAccount(ctx, entry, session, provider, model, kind, affinity, reviewParentThread(input, headers, model, kind))
	if id != "" {
		if account, lookupErr := s.accounts.Get(ctx, id); lookupErr == nil {
			entry.record.Provider = account.Provider
		} else if err == nil {
			err = lookupErr
		}
	}
	if err != nil {
		return nil, err
	}
	outgoing := headers.Clone()
	if outgoing == nil {
		outgoing = make(http.Header)
	}
	// Isolate provider-side session and prompt-cache identifiers between SubLane members.
	outgoing.Set("Session_id", fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16]))
	if kind == Messages || kind.IsGemini() || kind.IsImage() {
		delete(input, "prompt_cache_key")
	} else {
		input["prompt_cache_key"], _ = json.Marshal(hex.EncodeToString(digest[:]))
	}
	raw, err = json.Marshal(input)
	if err != nil {
		return nil, upstream.ErrInput
	}
	credential, err := s.accounts.Prepare(ctx, id, s.provider.Refresh)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	upstreamCtx := ctx
	var releaseUpstream func()
	if entry.record.Provider == "codex" && !kind.codexOnly() {
		upstreamCtx, releaseUpstream = drainingUpstreamContext(ctx, s.runContext, canceledStreamDrain)
		defer func() {
			if exchange == nil {
				releaseUpstream()
			}
		}()
	}
	// Use the local resource ID here; a provider credential's account ID may identify a user.
	upstreamCtx = logging.WithLogger(upstreamCtx, logging.From(ctx).With("request_id", entry.record.RequestID, "tenant_id", s.tenantID, "account_id", id, "provider", entry.record.Provider))
	executeOnce := func(c accounts.Credential) (*upstream.Stream, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if kind.IsGemini() {
			return s.provider.Gemini(upstreamCtx, c, raw, outgoing, kind == GeminiStream)
		}
		if kind == Messages {
			return s.provider.Messages(upstreamCtx, c, raw, outgoing)
		}
		if kind == Chat {
			return s.provider.Chat(upstreamCtx, c, raw, outgoing)
		}
		if kind.IsImage() {
			return s.provider.Images(upstreamCtx, c, raw, outgoing, kind == ImageEdits)
		}
		return s.provider.Responses(upstreamCtx, c, raw, outgoing, kind == Compact)
	}
	execute := func(c accounts.Credential) (*upstream.Stream, error) {
		return s.executeWithRetry(ctx, entry, id, model, original, c, executeOnce)
	}
	if entry.record.Provider == "codex" {
		row, err := s.queries.GetAccountUsage(ctx, id)
		if err != nil {
			return nil, err
		}
		entry.quotaRevision = row.Revision
	}
	entry.quotaReadStartedAt = s.now().UnixMilli()
	entry.upstreamDispatched = true
	result, err := execute(credential)
	if err != nil {
		return nil, err
	}
	if result.StatusCode == 401 {
		result.Body.Close()
		credential, err = s.accounts.RefreshAfterRejection(ctx, id, credential.AccessToken, s.provider.Refresh)
		if err != nil {
			return nil, err
		}
		result, err = execute(credential)
		if err != nil {
			return nil, err
		}
	}
	if result.StatusCode == 401 {
		_ = s.accounts.RecordUse(ctx, id, credential.AccessToken, false)
	}
	if result.StatusCode >= 200 && result.StatusCode < 300 {
		_ = s.accounts.RecordUse(ctx, id, credential.AccessToken, true)
	}
	return trackExchange(result, entry, upstreamCtx, releaseUpstream), nil
}

func drainingUpstreamContext(clientCtx, runCtx context.Context, grace time.Duration) (context.Context, func()) {
	base := context.WithoutCancel(clientCtx)
	var upstreamCtx context.Context
	var cancel context.CancelFunc
	if deadline, ok := clientCtx.Deadline(); ok {
		upstreamCtx, cancel = context.WithDeadline(base, deadline)
	} else {
		upstreamCtx, cancel = context.WithCancel(base)
	}
	stopClient := context.AfterFunc(clientCtx, func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-upstreamCtx.Done():
		}
	})
	stopRuntime := context.AfterFunc(runCtx, cancel)
	return upstreamCtx, func() { stopClient(); stopRuntime(); cancel() }
}

// Account reads share the same refresh owner and stale-token rejection rules as forwarding.
func readAccount[T any](ctx context.Context, s *Service, id string, read func(context.Context, accounts.Credential) (T, error)) (T, error) {
	var empty T
	credential, err := s.accounts.Prepare(ctx, id, s.provider.Refresh)
	if err != nil {
		return empty, err
	}
	result, err := read(ctx, credential)
	var rejected *upstream.UpstreamError
	if errors.As(err, &rejected) && rejected.Status == 401 {
		credential, err = s.accounts.RefreshAfterRejection(ctx, id, credential.AccessToken, s.provider.Refresh)
		if err != nil {
			return empty, err
		}
		result, err = read(ctx, credential)
	}
	if err == nil {
		if err := s.accounts.RecordUse(ctx, id, credential.AccessToken, true); err != nil {
			return empty, err
		}
	}
	if errors.As(err, &rejected) && rejected.Status == 401 {
		_ = s.accounts.RecordUse(ctx, id, credential.AccessToken, false)
	}
	return result, err
}

func poolAccounts(ctx context.Context, queries *db.Queries, userID, groupID int64) (map[string]bool, error) {
	allowed, err := queries.CanUseGroup(ctx, db.CanUseGroupParams{UserID: userID, GroupID: groupID})
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, groups.ErrUnavailable
	}
	ids, err := queries.ListGroupAccounts(ctx, groupID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
}
