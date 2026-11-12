package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/channels"
	"github.com/murongg/SubLane/internal/gateway"
)

type channelHTTP struct {
	service   *channels.Service
	resources *accounts.Service
	gateway   *gateway.Service
}

func (h *channelHTTP) register(router chi.Router) {
	routeErrors(router)
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h.resources == nil {
				writeJSON(w, 503, map[string]string{"error": "unavailable"})
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/", h.list)
	router.Post("/", h.create)
	router.Get("/runtime", h.runtime)
	router.Route("/{id}", func(route chi.Router) {
		route.Use(h.requireChannel)
		route.Get("/", h.get)
		route.Patch("/", h.setEnabled)
		route.Put("/", h.update)
		route.Delete("/", h.remove)
		route.Put("/key", h.rotate)
		route.Put("/proxy", h.bindProxy)
		route.Patch("/limits", h.limits)
		route.Post("/check", h.check)
		// Discovery and cooldowns share the same execution owner for both resource kinds.
		common := &accountHTTP{service: h.resources, gateway: h.gateway}
		route.Post("/resume", common.resume)
		route.Get("/models", common.catalog)
		route.Post("/models/refresh", common.refreshCatalog)
		route.Post("/models/check", common.checkModel)
		// Usage is reported only by platforms with a key-authenticated quota endpoint.
		route.Get("/usage", common.usage)
		route.Post("/usage/refresh", common.refreshUsage)
	})
}

func (h *channelHTTP) requireChannel(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := h.service.Get(r.Context(), chi.URLParam(r, "id")); err != nil {
			channelError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func channelError(w http.ResponseWriter, err error) {
	if errors.Is(err, channels.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "channel_not_found"})
		return
	}
	accountError(w, err)
}

func (h *channelHTTP) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.List(r.Context())
	if err != nil {
		channelError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"channels": rows})
}
func (h *channelHTTP) get(w http.ResponseWriter, r *http.Request) {
	row, err := h.service.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		channelError(w, err)
		return
	}
	writeJSON(w, 200, row)
}
func (h *channelHTTP) create(w http.ResponseWriter, r *http.Request) { h.save(w, r, "") }
func (h *channelHTTP) rotate(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, chi.URLParam(r, "id"))
}
func (h *channelHTTP) save(w http.ResponseWriter, r *http.Request, id string) {
	var input channels.Input
	if !decodeJSONLimit(w, r, &input, 32<<10) {
		return
	}
	row, err := h.service.Save(r.Context(), id, input)
	if err != nil {
		channelError(w, err)
		return
	}
	status := 200
	if id == "" {
		status = 201
	}
	writeJSON(w, status, row)
	if h.gateway != nil {
		_, _ = h.gateway.AccountCatalog(r.Context(), row.ID, false)
	}
}
func (h *channelHTTP) update(w http.ResponseWriter, r *http.Request) {
	var input channels.Input
	if !decodeJSONLimit(w, r, &input, 32<<10) {
		return
	}
	row, err := h.service.Update(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		channelError(w, err)
		return
	}
	writeJSON(w, 200, row)
	// A replacement key may expose a different model set; refresh discovery like a key rotation.
	if h.gateway != nil && input.APIKey != "" {
		_, _ = h.gateway.AccountCatalog(r.Context(), row.ID, false)
	}
}
func (h *channelHTTP) setEnabled(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		channelError(w, accounts.ErrInput)
		return
	}
	row, err := h.service.SetEnabled(r.Context(), chi.URLParam(r, "id"), *input.Enabled)
	if err != nil {
		channelError(w, err)
		return
	}
	writeJSON(w, 200, row)
}
func (h *channelHTTP) remove(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := h.service.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		channelError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *channelHTTP) bindProxy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProxyID string `json:"proxy_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	row, err := h.service.BindProxy(r.Context(), chi.URLParam(r, "id"), input.ProxyID)
	if err != nil {
		channelError(w, err)
		return
	}
	writeJSON(w, 200, row)
}
func (h *channelHTTP) limits(w http.ResponseWriter, r *http.Request) {
	var input struct {
		MaxConcurrency int64 `json:"max_concurrency"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if h.gateway == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.gateway.SetConcurrency(r.Context(), id, input.MaxConcurrency); err != nil {
		channelError(w, err)
		return
	}
	h.get(w, r)
}
func (h *channelHTTP) check(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	if h.gateway == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	models, err := h.gateway.Check(r.Context(), id)
	if err != nil {
		channelError(w, err)
		return
	}
	row, err := h.service.Get(r.Context(), id)
	if err != nil {
		channelError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"channel": row, "models": models})
}
func (h *channelHTTP) runtime(w http.ResponseWriter, r *http.Request) {
	if h.gateway == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	rows, err := h.service.List(r.Context())
	if err != nil {
		channelError(w, err)
		return
	}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[row.ID] = true
	}
	values, err := h.gateway.Runtime(r.Context())
	if err != nil {
		channelError(w, err)
		return
	}
	filtered := values[:0]
	for _, value := range values {
		if ids[value.ID] {
			filtered = append(filtered, value)
		}
	}
	writeJSON(w, 200, map[string]any{"channels": filtered, "server_time": time.Now().Unix()})
}
