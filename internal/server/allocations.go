package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/gateway"
)

type allocationHTTP struct{ gateway *gateway.Service }

func (h *allocationHTTP) available(w http.ResponseWriter, r *http.Request) bool {
	if h.gateway != nil {
		if err := h.gateway.PrepareAllocations(r.Context()); err != nil {
			writeJSON(w, 503, map[string]string{"error": "allocation_accounting_unavailable"})
			return false
		}
		return true
	}
	writeJSON(w, 503, map[string]string{"error": "unavailable"})
	return false
}
func allocationError(w http.ResponseWriter, err error) {
	status, code := 503, "unavailable"
	switch {
	case errors.Is(err, allocations.ErrInput):
		status, code = 400, err.Error()
	case errors.Is(err, allocations.ErrUnknownWindowOverride):
		status, code = 400, err.Error()
	case errors.Is(err, allocations.ErrNotFound):
		status, code = 404, err.Error()
	case errors.Is(err, allocations.ErrUnavailable):
		status, code = 403, err.Error()
	case errors.Is(err, allocations.ErrPoolConflict), errors.Is(err, allocations.ErrInFlight), errors.Is(err, allocations.ErrSettlement), errors.Is(err, allocations.ErrPending), errors.Is(err, allocations.ErrUnpriced):
		status, code = 409, err.Error()
	case errors.Is(err, allocations.ErrVersionRequired), errors.Is(err, allocations.ErrVersionConflict), errors.Is(err, allocations.ErrEffectiveRequired), errors.Is(err, allocations.ErrEffectiveConflict), errors.Is(err, allocations.ErrStateConflict), errors.Is(err, allocations.ErrMemberConflict), errors.Is(err, allocations.ErrFunding), errors.Is(err, allocations.ErrReplacement), errors.Is(err, allocations.ErrInitialSchedule):
		status, code = 409, err.Error()
	}
	writeJSON(w, status, map[string]string{"error": code})
}
func (h *allocationHTTP) register(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", func(w http.ResponseWriter, r *http.Request) { h.save(w, r, 0) })
	r.Post("/preview", h.previewInitial)
	r.Get("/{id}", h.detail)
	r.Patch("/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil || id <= 0 {
			allocationError(w, allocations.ErrInput)
			return
		}
		h.save(w, r, id)
	})
	r.Post("/{id}/settle", h.settle)
	r.Post("/{id}/enabled", h.enabled)
	r.Delete("/{id}", h.remove)
	r.Post("/{id}/members", h.members)
	r.Post("/{id}/preview", h.preview)
	r.Post("/{id}/cancel", func(w http.ResponseWriter, r *http.Request) { h.schedule(w, r, "cancel") })
	r.Post("/{id}/reschedule", func(w http.ResponseWriter, r *http.Request) { h.schedule(w, r, "reschedule") })
}

func (h *allocationHTTP) previewInitial(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in allocations.SchemeInput
	if !decodeJSONLimit(w, r, &in, 64<<10) {
		return
	}
	out, err := h.gateway.Allocations().PreviewInitial(r.Context(), in)
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (h *allocationHTTP) members(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	var in allocations.MembersInput
	if !decodeJSONLimit(w, r, &in, 64<<10) {
		return
	}
	out, err := h.gateway.Allocations().UpdateMembers(r.Context(), id, in)
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (h *allocationHTTP) preview(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Action == "" {
		in.Action = "schedule"
	}
	out, err := h.gateway.Allocations().Preview(r.Context(), id, in.Action)
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (h *allocationHTTP) schedule(w http.ResponseWriter, r *http.Request, action string) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	var in allocations.Conditions
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := h.gateway.Allocations().ChangeSchedule(r.Context(), id, action, in)
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *allocationHTTP) list(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	rows, err := h.gateway.Allocations().Schemes(r.Context())
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"schemes": rows})
}
func (h *allocationHTTP) save(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.available(w, r) {
		return
	}
	var in allocations.SchemeInput
	if !decodeJSONLimit(w, r, &in, 64<<10) {
		return
	}
	value, err := h.gateway.Allocations().SaveScheme(r.Context(), id, in)
	if err != nil {
		allocationError(w, err)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	writeJSON(w, status, value)
}
func (h *allocationHTTP) detail(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	value, err := h.gateway.Allocations().Detail(r.Context(), id, 0)
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h *allocationHTTP) own(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	values, err := h.gateway.Allocations().Own(r.Context(), sessionUser(r).ID)
	if err != nil {
		allocationError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"schemes": values})
}
func (h *allocationHTTP) settle(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	var in struct {
		RequestID string `json:"request_id"`
		Input     *int64 `json:"input"`
		Output    *int64 `json:"output"`
		Cached    *int64 `json:"cached"`
		// ImageInput is the optional image share of Input for image model requests.
		ImageInput *int64 `json:"image_input"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Input == nil || in.Output == nil || in.Cached == nil || in.ImageInput != nil && *in.ImageInput < 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	var imageInput int64
	if in.ImageInput != nil {
		imageInput = *in.ImageInput
	}
	err = h.gateway.Allocations().Settle(r.Context(), id, in.RequestID, allocations.Completion{Input: *in.Input, ImageInput: imageInput, Output: *in.Output, Cached: *in.Cached})
	if err != nil {
		allocationError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *allocationHTTP) remove(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrNotFound)
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err = h.gateway.Allocations().Delete(r.Context(), id); err != nil {
		allocationError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *allocationHTTP) enabled(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil || id <= 0 {
		allocationError(w, allocations.ErrInput)
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		allocationError(w, allocations.ErrInput)
		return
	}
	if err = h.gateway.Allocations().SetEnabled(r.Context(), id, *in.Enabled); err != nil {
		allocationError(w, err)
		return
	}
	w.WriteHeader(204)
}
