package gateway

import (
	"math"
	"time"

	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

type ResetWeight struct {
	Factor           float64  `json:"factor"`
	Reason           string   `json:"reason"`
	Window           string   `json:"window"`
	ResetAt          *int64   `json:"reset_at"`
	WindowSeconds    *int64   `json:"window_seconds"`
	RemainingPercent *float64 `json:"remaining_percent"`
}

func resetWeight(value upstream.Usage, now time.Time, p groups.SchedulingPolicy) ResetWeight {
	result := ResetWeight{Factor: 1, Reason: "disabled"}
	if !p.Enabled {
		return result
	}
	result.Reason = "unavailable"
	decision := quotaStatus(value, now)
	if decision.State != "available" || decision.Weight == 0 {
		return result
	}
	var main *upstream.UsageLimit
	for i := range value.Limits {
		if value.Limits[i].Name == "" {
			if main != nil {
				return result
			}
			main = &value.Limits[i]
		}
	}
	if main == nil || len(main.Windows) == 0 {
		return result
	}
	lowest := 101.0
	for _, w := range main.Windows {
		if w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || *w.UsedPercent >= 100 {
			return result
		}
		lowest = min(lowest, 100-*w.UsedPercent)
	}
	first := true
	for _, w := range main.Windows {
		remaining := 100 - *w.UsedPercent
		if math.Abs(remaining-lowest) > 1e-9 {
			continue
		}
		v := ResetWeight{Factor: 1, Reason: "timing_unknown", Window: w.Kind, ResetAt: w.ResetAt, WindowSeconds: w.WindowSeconds, RemainingPercent: &remaining}
		if w.ResetAt != nil && w.WindowSeconds != nil && *w.WindowSeconds > 0 {
			t := *w.ResetAt - now.Unix()
			if t > 0 && t <= *w.WindowSeconds {
				h := min(float64(p.MaxHorizonSeconds), p.HorizonFraction*float64(*w.WindowSeconds))
				x := max(0, min(1, 1-float64(t)/h))
				v.Factor = 1 + (p.MaxMultiplier-1)*math.Pow(x, p.CurvePower)
				v.Reason = "outside_horizon"
				if x > 0 {
					v.Reason = "approaching_reset"
				}
			}
		}
		// Equal constraints use the least reward, including unknown timing.
		if first || v.Factor < result.Factor {
			result = v
			first = false
		}
	}
	return result
}
