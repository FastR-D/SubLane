package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
)

var ErrUsageUnsupported = errors.New("provider_usage_unsupported")

type Usage struct {
	ReadStartedAt int64        `json:"read_started_at,omitempty"`
	Limits        []UsageLimit `json:"limits"`
	ResetCredits  *int64       `json:"reset_credits"`
	UpdatedAt     int64        `json:"updated_at"`
}

type UsageLimit struct {
	Name         string        `json:"name"`
	Hidden       bool          `json:"hidden,omitempty"`
	Allowed      *bool         `json:"allowed"`
	LimitReached *bool         `json:"limit_reached"`
	Windows      []UsageWindow `json:"windows"`
}

type UsageWindow struct {
	Kind          string   `json:"kind"`
	UsedPercent   *float64 `json:"used_percent"`
	WindowSeconds *int64   `json:"window_seconds"`
	ResetAt       *int64   `json:"reset_at"`
}

type usageDetails struct {
	Allowed      *bool        `json:"allowed"`
	LimitReached *bool        `json:"limit_reached"`
	Primary      *usageWindow `json:"primary_window"`
	Secondary    *usageWindow `json:"secondary_window"`
}

type usageWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowSeconds *int64   `json:"limit_window_seconds"`
	ResetAt       *int64   `json:"reset_at"`
	ResetAfter    *int64   `json:"reset_after_seconds"`
}

func (c *Client) Usage(ctx context.Context, credential accounts.Credential) (Usage, error) {
	ctx = credentialContext(ctx, credential)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch credential.Kind() {
	case "xai":
		return c.grokUsage(ctx, credential)
	case "claude":
		return c.claudeUsage(ctx, credential)
	case "antigravity":
		return c.antigravityUsage(ctx, credential)
	case "openai":
		// API channels only report quota where the platform publishes a key-authenticated usage endpoint.
		if OfficialOpenCodeGo(credential.BaseURL, credential.APIConfig().Platform) {
			return c.openCodeGoUsage(ctx, credential)
		}
		return Usage{}, ErrUsageUnsupported
	case "codex":
	default:
		return Usage{}, ErrUsageUnsupported
	}
	// Match the ChatGPT backend path used by openai/codex's backend-client, independent of model forwarding.
	request, err := http.NewRequestWithContext(ctx, "GET", "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return Usage{}, ErrInput
	}
	upstreamHeaders(request, credential, nil, false, c.codexVersion())
	response, err := c.http.Do(request)
	if err != nil {
		return Usage{}, ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Usage{}, &UpstreamError{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	raw, err := readBounded(response.Body, 128<<10)
	if err != nil {
		return Usage{}, ErrResponse
	}
	var payload struct {
		RateLimit    json.RawMessage `json:"rate_limit"`
		ResetCredits json.RawMessage `json:"rate_limit_reset_credits"`
		Additional   []struct {
			Name      string        `json:"limit_name"`
			RateLimit *usageDetails `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.RateLimit) == 0 || len(payload.Additional) > 32 {
		return Usage{}, ErrResponse
	}
	var primary *usageDetails
	if json.Unmarshal(payload.RateLimit, &primary) != nil {
		return Usage{}, ErrResponse
	}
	result := Usage{Limits: []UsageLimit{}, UpdatedAt: time.Now().Unix()}
	var embeddedCredits struct {
		AvailableCount *int64 `json:"available_count"`
	}
	// Optional reset-card metadata must not invalidate a usable quota snapshot.
	if json.Unmarshal(payload.ResetCredits, &embeddedCredits) == nil &&
		embeddedCredits.AvailableCount != nil && *embeddedCredits.AvailableCount >= 0 {
		result.ResetCredits = embeddedCredits.AvailableCount
	}
	appendLimit := func(name string, details *usageDetails) error {
		if details == nil {
			return nil
		}
		if len(name) > 256 {
			return ErrResponse
		}
		limit := UsageLimit{Name: name, Allowed: details.Allowed, LimitReached: details.LimitReached, Windows: []UsageWindow{}}
		for i, value := range []*usageWindow{details.Primary, details.Secondary} {
			if value == nil {
				continue
			}
			if value.UsedPercent != nil && *value.UsedPercent < 0 ||
				value.WindowSeconds != nil && (*value.WindowSeconds <= 0 || *value.WindowSeconds > 366*24*3600) ||
				value.ResetAt != nil && (*value.ResetAt < 0 || *value.ResetAt > 253402300799) ||
				value.ResetAfter != nil && (*value.ResetAfter < 0 || *value.ResetAfter > 366*24*3600) {
				return ErrResponse
			}
			// Missing values stay unknown; a missing percentage must never imply a full quota.
			resetAt := value.ResetAt
			if resetAt == nil && value.ResetAfter != nil {
				reset := result.UpdatedAt + *value.ResetAfter
				resetAt = &reset
			}
			kind := "primary"
			if i == 1 {
				kind = "secondary"
			}
			limit.Windows = append(limit.Windows, UsageWindow{Kind: kind, UsedPercent: value.UsedPercent, WindowSeconds: value.WindowSeconds, ResetAt: resetAt})
		}
		result.Limits = append(result.Limits, limit)
		return nil
	}
	if err := appendLimit("", primary); err != nil {
		return Usage{}, err
	}
	for _, extra := range payload.Additional {
		if err := appendLimit(extra.Name, extra.RateLimit); err != nil {
			return Usage{}, err
		}
	}
	if count := c.resetCredits(ctx, credential); count != nil {
		result.ResetCredits = count
	}
	return result, nil
}

func (c *Client) resetCredits(ctx context.Context, credential accounts.Credential) *int64 {
	raw, err := c.resetCardPayload(ctx, credential)
	if err != nil {
		return nil
	}
	var details struct {
		AvailableCount *int64 `json:"available_count"`
	}
	if json.Unmarshal(raw, &details) != nil || details.AvailableCount != nil && *details.AvailableCount < 0 {
		return nil
	}
	return details.AvailableCount
}
