package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/upstream"
)

type proxyHTTP struct {
	service *accounts.Service
	check   func(context.Context, string) (upstream.ProxyCheck, error)
}

func (h *proxyHTTP) register(router chi.Router) {
	routeErrors(router)
	router.Get("/", h.list)
	router.Post("/", h.create)
	router.Post("/import", h.importBatch)
	router.Post("/prune", h.pruneFailed)
	router.Post("/{id}/check", h.checkOne)
	router.Get("/{id}/ip-report", h.ipReport)
	router.Post("/{id}/ip-report/refresh", h.refreshIPReport)
	router.Put("/{id}", h.update)
	router.Delete("/{id}", h.remove)
}

func (h *proxyHTTP) pruneFailed(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	deleted, err := h.service.DeleteFailedProxies(r.Context())
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"deleted_count": deleted})
}

func (h *proxyHTTP) importBatch(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if !decodeJSONLimit(w, r, &input, 145<<10) {
		return
	}
	rows, err := h.service.ImportProxies(r.Context(), input.Text)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"proxies": rows})
}

func (h *proxyHTTP) checkOne(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	id := chi.URLParam(r, "id")
	address, revision, err := h.service.ProxyCheckTarget(r.Context(), id)
	if err != nil {
		accountError(w, err)
		return
	}
	check := h.check
	if check == nil {
		check = upstream.CheckProxy
	}
	result, err := check(r.Context(), address)
	if err != nil {
		accountError(w, err)
		return
	}
	proxy, err := h.service.SaveProxyCheck(r.Context(), id, revision, accounts.ProxyObservation{Reachable: result.Reachable, ExitIP: result.ExitIP, Country: result.Country, Region: result.Region, City: result.City, LatencyMS: result.LatencyMS, ErrorCode: result.ErrorCode})
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, proxy)
}

func (h *proxyHTTP) list(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	rows, err := h.service.ListProxies(r.Context())
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"proxies": rows})
}

func (h *proxyHTTP) create(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var input struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !decodeJSONLimit(w, r, &input, 8<<10) {
		return
	}
	proxy, err := h.service.CreateProxy(r.Context(), input.Name, input.URL)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 201, proxy)
}

func (h *proxyHTTP) update(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var input struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !decodeJSONLimit(w, r, &input, 8<<10) {
		return
	}
	proxy, err := h.service.UpdateProxy(r.Context(), chi.URLParam(r, "id"), input.Name, input.URL)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, proxy)
}

func (h *proxyHTTP) remove(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := h.service.DeleteProxy(r.Context(), chi.URLParam(r, "id")); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(204)
}
