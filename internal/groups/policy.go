package groups

import (
	"context"
	"regexp"
	"strings"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/storage/db"
)

type ModelPolicy struct {
	Routing          Routing  `json:"-"`
	HasSubscriptions bool     `json:"-"`
	Restricted       bool     `json:"restricted"`
	Models           []string `json:"models"`
}

var modelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/()+-]{0,127}$`)

// SplitModel recognizes legacy gateway prefixes while preserving slashes in native model IDs.
func SplitModel(model string) (provider, native string) {
	// Keep newly supported API model namespaces (for example openai/model) native, rather than inventing legacy prefixes.
	if provider, native, found := strings.Cut(model, "/"); found && accounts.SubscriptionProvider(provider) {
		return provider, native
	}
	return "", model
}
func (p ModelPolicy) normalized() (ModelPolicy, error) {
	if len(p.Models) > 100 {
		return ModelPolicy{}, ErrInput
	}
	result := ModelPolicy{Restricted: p.Restricted, Models: []string{}}
	seen := map[string]bool{}
	for _, id := range p.Models {
		id = strings.TrimSpace(id)
		_, native := SplitModel(id)
		if !modelID.MatchString(native) {
			return ModelPolicy{}, ErrInput
		}
		if !seen[id] {
			result.Models = append(result.Models, id)
			seen[id] = true
		}
	}
	return result, nil
}
func (p ModelPolicy) Allows(model string) bool {
	if !p.Restricted {
		return true
	}
	provider, native := SplitModel(model)
	for _, allowed := range p.Models {
		scope, name := SplitModel(allowed)
		// Bare requests may match any rule; selection must recheck the actual account provider.
		if name == native && (provider == "" || scope == "" || provider == scope) {
			return true
		}
	}
	return false
}

func (p ModelPolicy) AllowsResource(provider, model string) bool {
	if !p.AllowsProvider(provider) {
		return false
	}
	if !p.Restricted {
		return true
	}
	// API model namespaces are native IDs. Never prepend a provider and reinterpret them as legacy qualified requests.
	for _, permitted := range p.Models {
		scope, native := SplitModel(permitted)
		if native == model && (scope == "" || scope == provider) {
			return true
		}
	}
	return false
}

// ReadPolicy requires transaction-bound queries so grants and the allowlist share one snapshot.
func ReadPolicy(ctx context.Context, q *db.Queries, userID, groupID int64) (ModelPolicy, error) {
	allowed, err := q.CanUseGroup(ctx, db.CanUseGroupParams{UserID: userID, GroupID: groupID})
	if err != nil {
		return ModelPolicy{}, err
	}
	if !allowed {
		return ModelPolicy{}, ErrUnavailable
	}
	group, err := q.GetGroup(ctx, groupID)
	if err != nil {
		return ModelPolicy{}, err
	}
	models, err := q.ListGroupModels(ctx, groupID)
	if err != nil {
		return ModelPolicy{}, err
	}
	hasSubscriptions, err := q.GroupHasSubscriptions(ctx, groupID)
	return ModelPolicy{Restricted: group.RestrictedModels, Models: models, Routing: Routing{Preference: group.RoutingPreference, AllowAPIFallback: group.AllowApiFallback != 0, SessionSticky: group.SessionSticky}, HasSubscriptions: hasSubscriptions}, err
}
