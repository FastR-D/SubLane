package groups

import "github.com/murongg/SubLane/internal/accounts"

type Resource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type Routing struct {
	Preference       string `json:"preference"`
	AllowAPIFallback bool   `json:"allow_api_fallback"`
	// SessionSticky derives conversation keys for sessionless clients and lets subagents inherit their parent's resource.
	SessionSticky bool `json:"session_sticky"`
}

func (r Routing) valid() bool {
	return r.Preference == "protocol" || r.Preference == "subscription_first" || r.Preference == "api_first"
}

func resourceKind(provider string) string {
	if provider == "openai" {
		return "channel"
	}
	return "subscription"
}

func resourceIDs(input Input) ([]string, map[string]string, error) {
	if input.Resources == nil {
		return input.AccountIDs, nil, nil
	}
	// account_ids is the compatibility contract; a request must choose one membership representation.
	if input.AccountIDs != nil || len(input.Resources) > 100 {
		return nil, nil, ErrInput
	}
	ids := make([]string, 0, len(input.Resources))
	kinds := make(map[string]string, len(input.Resources))
	for _, r := range input.Resources {
		if r.Kind != "subscription" && r.Kind != "channel" || r.ID == "" {
			return nil, nil, ErrInput
		}
		if _, exists := kinds[r.ID]; exists {
			return nil, nil, ErrInput
		}
		ids = append(ids, r.ID)
		kinds[r.ID] = r.Kind
	}
	return ids, kinds, nil
}

func (p ModelPolicy) AllowsProvider(provider string) bool {
	// Selecting only channels is an explicit API choice. Mixed groups need opt-in fallback when subscriptions are preferred.
	return !(provider == "openai" && p.HasSubscriptions && p.Routing.Preference == "subscription_first" && !p.Routing.AllowAPIFallback)
}

func (r Routing) Rank(provider, preferred string) int {
	base := 0
	if r.Preference == "subscription_first" && provider == "openai" || r.Preference == "api_first" && accounts.SubscriptionProvider(provider) {
		base = 2
	}
	if provider != preferred {
		base++
	}
	return base
}
