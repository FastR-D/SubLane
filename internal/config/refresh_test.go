package config

import (
	"testing"
	"time"
)

func TestCredentialRefreshPolicy(t *testing.T) {
	for _, tc := range []struct {
		lead, interval string
		valid          bool
	}{
		{"", "", true}, {"24h", "2m", true}, {"2m", "0s", true},
		{"1m", "1m", false}, {"169h", "1m", false}, {"invalid", "1m", false},
		{"96h", "-1s", false}, {"96h", "1s", false}, {"96h", "25h", false},
	} {
		cfg, err := Load(func(k string) string {
			switch k {
			case "SUBLANE_CODEX_REFRESH_LEAD":
				return tc.lead
			case "SUBLANE_ACCOUNT_REFRESH_INTERVAL":
				return tc.interval
			}
			return ""
		})
		if (err == nil) != tc.valid {
			t.Fatalf("lead=%q interval=%q: %v", tc.lead, tc.interval, err)
		}
		if tc.lead == "" && err == nil && (cfg.CodexRefreshLead != 96*time.Hour || cfg.AccountRefreshInterval != time.Minute) {
			t.Fatal("unsafe refresh defaults")
		}
	}
}
