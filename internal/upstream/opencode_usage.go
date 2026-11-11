package upstream

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
)

// OpenCodeGoUsageURL is the official JSON usage endpoint; it accepts the channel's API key.
var OpenCodeGoUsageURL = "https://opencode.ai/zen/go/v1/usage"

// OfficialOpenCodeGo reports whether a channel's key belongs to the official OpenCode origin.
// The preset label alone is not enough: a custom relay key must never be sent to opencode.ai.
func OfficialOpenCodeGo(baseURL, platform string) bool {
	if platform != "opencode-go" {
		return false
	}
	u, err := url.Parse(baseURL)
	return err == nil && u.Scheme == "https" && u.User == nil && strings.EqualFold(u.Host, "opencode.ai")
}

const (
	openCodeGoRollingSeconds int64 = 5 * 3600
	openCodeGoWeeklySeconds  int64 = 7 * 24 * 3600
)

// openCodeGoUsage maps the rolling, weekly and monthly spending windows onto one main limit.
// Missing windows stay absent and missing percentages stay unknown.
func (c *Client) openCodeGoUsage(ctx context.Context, credential accounts.Credential) (Usage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, OpenCodeGoUsageURL, nil)
	if err != nil {
		return Usage{}, ErrInput
	}
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("Accept", "application/json")
	raw, err := c.subscriptionUsageResponse(request)
	if err != nil {
		return Usage{}, err
	}
	type window struct {
		Status   string          `json:"status"`
		Percent  *float64        `json:"percent"`
		ResetsAt json.RawMessage `json:"resetsAt"`
	}
	var payload struct {
		Usage *struct {
			Rolling *window `json:"rolling"`
			Weekly  *window `json:"weekly"`
			Monthly *window `json:"monthly"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Usage == nil {
		return Usage{}, ErrResponse
	}
	result := Usage{Limits: []UsageLimit{}, UpdatedAt: time.Now().Unix()}
	limit := UsageLimit{Windows: []UsageWindow{}}
	reached := false
	rolling, weekly := openCodeGoRollingSeconds, openCodeGoWeeklySeconds
	for _, entry := range []struct {
		value   *window
		kind    string
		seconds *int64
	}{
		{payload.Usage.Rolling, "primary", &rolling},
		{payload.Usage.Weekly, "secondary", &weekly},
		{payload.Usage.Monthly, "monthly", nil},
	} {
		if entry.value == nil {
			continue
		}
		used := entry.value.Percent
		if used != nil && (math.IsNaN(*used) || math.IsInf(*used, 0) || *used < 0) {
			return Usage{}, ErrResponse
		}
		reset, err := subscriptionResetAt(entry.value.ResetsAt)
		if err != nil {
			return Usage{}, err
		}
		if used != nil && *used >= 100 {
			reached = true
		}
		limit.Windows = append(limit.Windows, UsageWindow{Kind: entry.kind, UsedPercent: used, WindowSeconds: entry.seconds, ResetAt: reset})
	}
	if len(limit.Windows) > 0 {
		limit.LimitReached = &reached
		result.Limits = append(result.Limits, limit)
	}
	return result, nil
}
