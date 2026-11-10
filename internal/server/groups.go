package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/groups"
)

type groupHTTP struct {
	service *groups.Service
	gateway *gateway.Service
}

func (h *groupHTTP) register(router chi.Router) {
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h.service == nil {
				writeJSON(w, 503, map[string]string{"error": "unavailable"})
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/", h.list)
	router.Post("/", h.create)
	router.Get("/scheduling-policy", h.schedulingPolicy)
	router.Put("/scheduling-policy", h.saveSchedulingPolicy)
	router.Get("/scheduling-policy/revisions", h.schedulingRevisions)
	router.Get("/{id}", h.get)
	router.Get("/{id}/members", h.poolMembers)
	router.Get("/{id}/models", h.catalog)
	router.Get("/{id}/scheduling", h.scheduling)
	router.Post("/{id}/scheduling/preview", h.previewSchedulingPolicy)
	router.Get("/{id}/scheduling/history", h.schedulingHistory)
	router.Get("/{id}/models/availability", h.availability)
	router.Patch("/{id}", h.update)
	router.Get("/members/{id}", h.memberGroups)
	router.Put("/members/{id}", h.setMemberGroups)
}
func (h *groupHTTP) list(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.List(r.Context())
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"groups": values})
}
func (h *groupHTTP) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	value, err := h.service.Get(r.Context(), id)
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h *groupHTTP) poolMembers(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	values, err := h.service.Members(r.Context(), id)
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"members": values})
}
func (h *groupHTTP) create(w http.ResponseWriter, r *http.Request) { h.save(w, r, 0) }
func (h *groupHTTP) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	h.save(w, r, id)
}
func (h *groupHTTP) save(w http.ResponseWriter, r *http.Request, id int64) {
	var input struct {
		PreferenceBoosts map[string]int64    `json:"preference_boosts"`
		Resources        []groups.Resource   `json:"resources"`
		Routing          *groups.Routing     `json:"routing"`
		ModelPolicy      *groups.ModelPolicy `json:"model_policy"`
		Name             string              `json:"name"`
		Enabled          *bool               `json:"enabled"`
		AccountIDs       []string            `json:"account_ids"`
	}
	if !decodeJSONLimit(w, r, &input, 32<<10) {
		return
	}
	if input.Enabled == nil {
		groupError(w, groups.ErrInput)
		return
	}
	value, err := h.service.Save(r.Context(), id, groups.Input{PreferenceBoosts: input.PreferenceBoosts, Resources: input.Resources, Routing: input.Routing, ModelPolicy: input.ModelPolicy, Name: input.Name, Enabled: *input.Enabled, AccountIDs: input.AccountIDs})
	if err != nil {
		groupError(w, err)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	writeJSON(w, status, value)
}
func (h *groupHTTP) memberGroups(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	ids, err := h.service.MemberGroups(r.Context(), id)
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"group_ids": ids})
}
func (h *groupHTTP) setMemberGroups(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	var input struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := h.service.SetMemberGroups(r.Context(), id, input.GroupIDs); err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"group_ids": input.GroupIDs})
}
func (h *keyHTTP) groupChoices(w http.ResponseWriter, r *http.Request) {
	if h.groups == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	values, err := h.groups.Available(r.Context(), sessionUser(r).ID)
	if err != nil {
		groupError(w, err)
		return
	}
	if h.gateway != nil {
		all, err := h.gateway.Allocations().Schemes(r.Context())
		if err != nil {
			allocationError(w, err)
			return
		}
		own, err := h.gateway.Allocations().Own(r.Context(), sessionUser(r).ID)
		if err != nil {
			allocationError(w, err)
			return
		}
		eligible := map[int64]allocations.Detail{}
		for _, s := range own {
			if s.Available {
				eligible[s.ID] = s
			}
		}
		filtered := make([]groups.Choice, 0, len(values))
		for _, g := range values {
			managed := false
			for _, s := range all {
				if s.GroupID == g.ID {
					managed = true
					if available, ok := eligible[s.ID]; ok {
						g.SchemeID = s.ID
						g.SchemeName = available.Name
						filtered = append(filtered, g)
					}
					break
				}
			}
			if !managed {
				filtered = append(filtered, g)
			}
		}
		values = filtered
	}
	writeJSON(w, 200, map[string]any{"groups": values})
}
func groupError(w http.ResponseWriter, err error) {
	status, code := 503, "unavailable"
	switch {
	case errors.Is(err, groups.ErrAllocated):
		status, code = 409, err.Error()
	case errors.Is(err, groups.ErrInput):
		status, code = 400, "invalid_group_input"
	case errors.Is(err, groups.ErrNotFound):
		status, code = 404, "group_not_found"
	case errors.Is(err, groups.ErrDuplicate):
		status, code = 409, "group_exists"
	case errors.Is(err, groups.ErrLimit):
		status, code = 409, "group_limit"
	case errors.Is(err, groups.ErrUnavailable):
		status, code = 403, "group_unavailable"
	}
	writeJSON(w, status, map[string]string{"error": code})
}

func (h *groupHTTP) scheduling(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	if h.gateway == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	if _, err := h.service.Get(r.Context(), id); err != nil {
		groupError(w, err)
		return
	}
	value, err := h.gateway.Scheduling(r.Context(), sessionUser(r).ID, id, r.URL.Query().Get("model"))
	if err != nil {
		gatewayError(w, err)
		return
	}
	writeJSON(w, 200, value)
}

func (h *groupHTTP) schedulingPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := h.service.SchedulingPolicy(r.Context())
	if err != nil {
		groupError(w, err)
		return
	}
	dynamic := h.gateway != nil && h.gateway.Policy().QuotaWeighted
	writeJSON(w, 200, map[string]any{"policy": p, "dynamic": dynamic})
}
func (h *groupHTTP) saveSchedulingPolicy(w http.ResponseWriter, r *http.Request) {
	var p groups.SchedulingPolicy
	if !decodeJSONLimit(w, r, &p, 2048) {
		return
	}
	value, err := h.service.SaveSchedulingPolicy(r.Context(), p)
	if errors.Is(err, groups.ErrSchedulingConflict) {
		writeJSON(w, 409, map[string]string{"error": "scheduling_policy_conflict"})
		return
	}
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h *groupHTTP) schedulingRevisions(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.SchedulingRevisions(r.Context())
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"revisions": values})
}

func (h *groupHTTP) schedulingHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	if h.gateway == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	if _, err = h.service.Get(r.Context(), id); err != nil {
		groupError(w, err)
		return
	}
	hours := int64(24)
	if raw := r.URL.Query().Get("hours"); raw != "" {
		hours, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			groupError(w, groups.ErrInput)
			return
		}
	}
	value, err := h.gateway.SchedulingHistory(r.Context(), sessionUser(r).ID, id, hours, r.URL.Query().Get("account"))
	if err != nil {
		gatewayError(w, err)
		return
	}
	writeJSON(w, 200, value)
}

func (h *groupHTTP) previewSchedulingPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil || id <= 0 {
		groupError(w, groups.ErrInput)
		return
	}
	if h.gateway == nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	if _, err = h.service.Get(r.Context(), id); err != nil {
		groupError(w, err)
		return
	}
	var input struct {
		Model  string                  `json:"model"`
		Policy groups.SchedulingPolicy `json:"policy"`
	}
	if !decodeJSONLimit(w, r, &input, 4096) {
		return
	}
	v, err := h.gateway.PreviewSchedulingPolicy(r.Context(), sessionUser(r).ID, id, input.Model, input.Policy)
	if errors.Is(err, groups.ErrSchedulingConflict) {
		writeJSON(w, 409, map[string]string{"error": "scheduling_policy_conflict"})
		return
	}
	if err != nil {
		gatewayError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
