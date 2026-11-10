package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/alerts"
	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/capacity"
	"github.com/murongg/SubLane/internal/content"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/metrics"
	"github.com/murongg/SubLane/internal/oauth"
	"github.com/murongg/SubLane/internal/pricing"
	"github.com/murongg/SubLane/internal/resets"
	"github.com/murongg/SubLane/internal/server"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/timezone"
	"github.com/murongg/SubLane/internal/turnstile"
	"github.com/murongg/SubLane/internal/updates"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
	"github.com/murongg/SubLane/internal/versions"
)

type tenantRuntime struct {
	handler  http.Handler
	gateway  *gateway.Service
	active   int
	lastUsed time.Time
}

func (r *tenantRegistry) Capacity(ctx context.Context, id int64) ([]capacity.Pool, error) {
	runtime := r.acquire(id)
	if runtime == nil {
		return nil, alerts.ErrCapacityUnavailable
	}
	defer r.release(runtime)
	return runtime.gateway.WorkspaceCapacity(ctx)
}

func (r *tenantRegistry) AllowanceAlerts(ctx context.Context, id int64) ([]alerts.Allowance, error) {
	runtime := r.acquire(id)
	if runtime == nil {
		return nil, alerts.ErrCapacityUnavailable
	}
	defer r.release(runtime)
	values, err := runtime.gateway.Allocations().ThresholdAlerts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.Allowance, 0, len(values))
	for _, value := range values {
		out = append(out, alerts.Allowance{Subject: fmt.Sprintf("%d:%d:%d:%d", value.SchemeID, value.UserID, value.WindowSeconds, value.Threshold), Cycle: value.Cycle})
	}
	return out, nil
}

const runtimeIdleTimeout = 5 * time.Minute

type tenantRegistry struct {
	metrics           *metrics.Collector
	mu                sync.Mutex
	runtimes          map[int64]*tenantRuntime
	ctx               context.Context
	db                *sql.DB
	vault             *vault.Vault
	auth              *auth.Service
	alerts            *alerts.Service
	resetCredits      *resets.Service
	tenants           *tenants.Service
	provider          *upstream.Client
	pricing           *pricing.Service
	versions          *versions.Service
	updates           *updates.Service
	grokVersions      *versions.Service
	claudeVersions    *versions.Service
	timeZone          *timezone.Service
	turnstile         *turnstile.Service
	assets            fs.FS
	dataDir           string
	keyFile           string
	publicURL         string
	trustedProxies    []netip.Prefix
	loginLimiter      *server.LoginLimiter
	enrollmentLimiter *server.LoginLimiter
	version           string
	started           time.Time
	now               func() time.Time
	closed            bool
	gatewayPolicy     gateway.Policy
	stopReap          chan struct{}
	reapDone          chan struct{}
}

func (r *tenantRegistry) Handler(id int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		runtime := r.acquire(id)
		if runtime == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		defer r.release(runtime)
		runtime.handler.ServeHTTP(w, request)
	})
}

func (r *tenantRegistry) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *tenantRegistry) acquire(id int64) *tenantRuntime {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if runtime, ok := r.runtimes[id]; ok {
		runtime.active++
		return runtime
	}
	if r.loginLimiter == nil {
		r.loginLimiter = server.NewLoginLimiter()
		r.enrollmentLimiter = server.NewLoginLimiter()
	}
	accountService := accounts.NewForTenant(r.db, r.vault, id)
	policy := r.gatewayPolicy
	if policy.Capacity == 0 {
		policy = gateway.DefaultPolicy()
	}
	forwarding := gateway.NewForTenantWithPolicy(r.ctx, r.db, accountService, r.provider, id, policy, r.pricing)
	forwarding.SetTelemetry(r.metrics)
	rules := content.New(r.db, r.vault, id)
	forwarding.SetContent(rules)
	forwarding.SetTimeZone(r.timeZone)
	forwarding.StartScheduling()
	dataDir := ""
	var codexVersions *versions.Service
	var grokVersions *versions.Service
	var claudeVersions *versions.Service
	if id == 1 {
		// The instance archive and version policy remain platform operations.
		dataDir, codexVersions, grokVersions, claudeVersions = r.dataDir, r.versions, r.grokVersions, r.claudeVersions
	}
	handler := server.New(server.Options{
		DataDir: dataDir, CredentialsKeyFile: r.keyFile, Assets: r.assets, Version: r.version, StartedAt: r.started,
		Ping: r.db.PingContext, Audit: audit.NewForTenant(r.db, id), Auth: r.auth,
		Alerts:  r.alerts,
		Content: rules,
		Keys:    apikey.NewForTenant(r.db, r.vault, id), Accounts: accountService,
		OAuth: oauth.New(accountService, r.provider), Gateway: forwarding, Pricing: r.pricing, ResetCredits: r.resetCredits,
		Groups: groups.NewForTenant(r.db, id), Tenants: r.tenants, TenantID: id,
		PublicURL: r.publicURL, TrustedProxies: r.trustedProxies, LoginLimiter: r.loginLimiter, EnrollmentLimiter: r.enrollmentLimiter,
		CodexVersions: codexVersions, GrokVersions: grokVersions, ClaudeVersions: claudeVersions, TimeZone: r.timeZone,
		Turnstile: r.turnstile, Updates: r.updates,
	})
	if r.runtimes == nil {
		r.runtimes = make(map[int64]*tenantRuntime)
	}
	runtime := &tenantRuntime{handler: handler, gateway: forwarding, active: 1, lastUsed: r.clock()}
	r.runtimes[id] = runtime
	return runtime
}

func (r *tenantRegistry) release(runtime *tenantRuntime) {
	r.mu.Lock()
	runtime.active--
	runtime.lastUsed = r.clock()
	r.mu.Unlock()
}

func (r *tenantRegistry) Start() {
	r.mu.Lock()
	if r.closed || r.stopReap != nil {
		r.mu.Unlock()
		return
	}
	r.stopReap = make(chan struct{})
	r.reapDone = make(chan struct{})
	location := func() *time.Location {
		if r.timeZone != nil {
			return r.timeZone.Location()
		}
		return time.UTC
	}
	r.resetCredits = resets.New(r.db, location, r.resetObservation, r.redeemResetCredit)
	r.resetCredits.Start(r.ctx)
	stop, done := r.stopReap, r.reapDone
	r.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				r.reapIdle()
			}
		}
	}()
}

func (r *tenantRegistry) reapIdle() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	cutoff := r.clock().Add(-runtimeIdleTimeout)
	var retired []*tenantRuntime
	for id, runtime := range r.runtimes {
		if runtime.active == 0 && !runtime.lastUsed.After(cutoff) {
			delete(r.runtimes, id)
			retired = append(retired, runtime)
		}
	}
	r.mu.Unlock()
	for _, runtime := range retired {
		runtime.gateway.Close()
	}
}

func (r *tenantRegistry) Close() {
	if r.resetCredits != nil {
		r.resetCredits.Close()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	if r.stopReap != nil {
		close(r.stopReap)
	}
	done := r.reapDone
	runtimes := r.runtimes
	r.runtimes = nil
	r.mu.Unlock()
	if done != nil {
		<-done
	}
	for _, runtime := range runtimes {
		runtime.gateway.Close()
	}
}

func (r *tenantRegistry) Pressure() (inFlight, queued int64) {
	r.mu.Lock()
	runtimes := make([]*tenantRuntime, 0, len(r.runtimes))
	for _, runtime := range r.runtimes {
		runtimes = append(runtimes, runtime)
	}
	r.mu.Unlock()
	for _, runtime := range runtimes {
		active, waiting := runtime.gateway.Pressure()
		inFlight += active
		queued += waiting
	}
	return inFlight, queued
}

func (r *tenantRegistry) resetObservation(ctx context.Context, tenant int64, id string) (resets.Observation, error) {
	runtime := r.acquire(tenant)
	if runtime == nil {
		return resets.Observation{}, resets.ErrBusy
	}
	defer r.release(runtime)
	return runtime.gateway.ResetObservation(ctx, id)
}

func (r *tenantRegistry) redeemResetCredit(ctx context.Context, tenant int64, id, key, cardID string) (string, error) {
	runtime := r.acquire(tenant)
	if runtime == nil {
		return "", resets.ErrBusy
	}
	defer r.release(runtime)
	return runtime.gateway.RedeemResetCredit(ctx, id, key, cardID)
}
