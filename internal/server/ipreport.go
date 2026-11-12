package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/murongg/SubLane/internal/upstream"
)

const (
	ipReportTTL      = 6 * time.Hour
	ipReportCooldown = 30 * time.Second
	ipReportBackoff  = 5 * time.Minute
	ipReportEntries  = 256
)

var (
	errNoExitIP        = errors.New("proxy_exit_ip_unknown")
	errIPReportMissing = errors.New("ip_report_not_loaded")
	errIPReportPaced   = errors.New("ip_report_rate_limited")
)

// ipReportCache keeps third-party reputation lookups bounded: one result per public exit IP,
// shared by every workspace because the data describes the IP rather than any tenant.
// Reads never contact the service; only an explicit lookup discloses an exit IP.
type ipReportCache struct {
	mu      sync.Mutex
	entries map[string]*ipReportEntry
	now     func() time.Time
	lookup  func(context.Context, string) (upstream.IPReport, error)
}

type ipReportEntry struct {
	report *upstream.IPReport
	at     time.Time
	// nextAttempt starts counting when an attempt begins, so failures and concurrent callers are paced too.
	nextAttempt time.Time
	flight      chan struct{}
	err         error
}

var ipReports = &ipReportCache{entries: map[string]*ipReportEntry{}, now: time.Now, lookup: upstream.LookupIPReport}

// cached returns a fresh stored report without any third-party request.
func (c *ipReportCache) cached(ip string) (upstream.IPReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[ip]; entry != nil && entry.report != nil && c.now().Sub(entry.at) < ipReportTTL {
		return *entry.report, nil
	}
	return upstream.IPReport{}, errIPReportMissing
}

// fetch performs an explicit lookup. Concurrent callers for one IP share a single upstream request.
func (c *ipReportCache) fetch(ctx context.Context, ip string) (upstream.IPReport, time.Duration, error) {
	c.mu.Lock()
	now := c.now()
	entry := c.entries[ip]
	if entry == nil {
		if !c.makeRoomLocked() {
			// Every slot holds an in-flight lookup; admitting another IP would exceed the bound.
			c.mu.Unlock()
			return upstream.IPReport{}, ipReportCooldown, errIPReportPaced
		}
		entry = &ipReportEntry{}
		c.entries[ip] = entry
	}
	if flight := entry.flight; flight != nil {
		c.mu.Unlock()
		select {
		case <-flight:
		case <-ctx.Done():
			return upstream.IPReport{}, 0, ctx.Err()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if entry.err != nil {
			return upstream.IPReport{}, 0, entry.err
		}
		return *entry.report, 0, nil
	}
	if now.Before(entry.nextAttempt) {
		wait := entry.nextAttempt.Sub(now)
		defer c.mu.Unlock()
		if entry.report != nil {
			return *entry.report, wait, nil
		}
		return upstream.IPReport{}, wait, errIPReportPaced
	}
	entry.nextAttempt = now.Add(ipReportCooldown)
	flight := make(chan struct{})
	entry.flight = flight
	c.mu.Unlock()
	// The shared lookup uses its own bounded context so one caller's cancellation cannot fail the others.
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	report, err := c.lookup(lookupCtx, ip)
	cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.flight, entry.err = nil, err
	close(flight)
	var upstreamErr *upstream.UpstreamError
	if errors.As(err, &upstreamErr) && upstreamErr.Status == http.StatusTooManyRequests {
		entry.nextAttempt = c.now().Add(ipReportBackoff)
	}
	if err != nil {
		return upstream.IPReport{}, 0, err
	}
	entry.report, entry.at = &report, c.now()
	return report, 0, nil
}

// makeRoomLocked reports whether a new entry fits, evicting the oldest idle entry when full.
// Called with the lock held; in-flight entries are never evicted.
func (c *ipReportCache) makeRoomLocked() bool {
	if len(c.entries) < ipReportEntries {
		return true
	}
	oldest := ""
	for key, value := range c.entries {
		if value.flight != nil {
			continue
		}
		if oldest == "" || value.nextAttempt.Before(c.entries[oldest].nextAttempt) {
			oldest = key
		}
	}
	if oldest == "" {
		return false
	}
	delete(c.entries, oldest)
	return true
}

func (h *proxyHTTP) exitIP(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return "", false
	}
	proxies, err := h.service.ListProxies(r.Context())
	if err != nil {
		accountError(w, err)
		return "", false
	}
	id := chi.URLParam(r, "id")
	for _, proxy := range proxies {
		if proxy.ID != id {
			continue
		}
		// Reports describe the exit observed by the last successful check, never the proxy endpoint.
		if proxy.ExitIP == "" {
			writeJSON(w, 409, map[string]string{"error": errNoExitIP.Error()})
			return "", false
		}
		return proxy.ExitIP, true
	}
	writeJSON(w, 404, map[string]string{"error": "proxy_not_found"})
	return "", false
}

// ipReport reads the cached report only; it never sends the exit IP anywhere.
func (h *proxyHTTP) ipReport(w http.ResponseWriter, r *http.Request) {
	ip, ok := h.exitIP(w, r)
	if !ok {
		return
	}
	report, err := ipReports.cached(ip)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, report)
}

// refreshIPReport is the explicit administrator action that discloses the exit IP to the service.
func (h *proxyHTTP) refreshIPReport(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	ip, ok := h.exitIP(w, r)
	if !ok {
		return
	}
	report, wait, err := ipReports.fetch(r.Context(), ip)
	if err != nil {
		status, code := 502, "ip_report_unavailable"
		var upstreamErr *upstream.UpstreamError
		switch {
		case errors.Is(err, upstream.ErrInput):
			status, code = 422, "ip_report_unsupported"
		case errors.Is(err, errIPReportPaced), errors.As(err, &upstreamErr) && upstreamErr.Status == http.StatusTooManyRequests:
			status, code = 429, "ip_report_rate_limited"
			if wait > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			}
		}
		writeJSON(w, status, map[string]string{"error": code})
		return
	}
	writeJSON(w, 200, report)
}
