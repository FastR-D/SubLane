package config

import (
	"fmt"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
)

func (c *Config) loadRefresh(getenv func(string) string) error {
	c.CodexRefreshLead, c.AccountRefreshInterval = accounts.CodexRefreshLead, time.Minute
	for key, target := range map[string]*time.Duration{
		"SUBLANE_CODEX_REFRESH_LEAD":       &c.CodexRefreshLead,
		"SUBLANE_ACCOUNT_REFRESH_INTERVAL": &c.AccountRefreshInterval,
	} {
		if value := getenv(key); value != "" {
			d, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s must be a duration", key)
			}
			*target = d
		}
	}
	if c.CodexRefreshLead < 2*time.Minute || c.CodexRefreshLead > 7*24*time.Hour {
		return fmt.Errorf("SUBLANE_CODEX_REFRESH_LEAD must be 2m–168h")
	}
	if c.AccountRefreshInterval != 0 && (c.AccountRefreshInterval < time.Minute || c.AccountRefreshInterval > 24*time.Hour) {
		return fmt.Errorf("SUBLANE_ACCOUNT_REFRESH_INTERVAL must be 0s or 1m–24h")
	}
	return nil
}
