package config

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/murongg/SubLane/internal/backup"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/vault"
)

type Config struct {
	Backup                 backup.Schedule
	BackupWebhookURL       string
	Demo                   bool
	Addr                   string
	DataDir                string
	CredentialsKeyFile     string
	LogLevel               slog.Level
	MaxRequestBody         int64
	PublicURL              string
	TrustedProxies         []netip.Prefix
	PricingURL             string
	PricingHashURL         string
	PricingCache           string
	PricingFallback        string
	PricingOverride        string
	PricingInterval        time.Duration
	MetricsToken           string
	UpdateDir              string
	Gateway                gateway.Policy
	InferenceHeaderTimeout time.Duration
	CodexRefreshLead       time.Duration
	AccountRefreshInterval time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{Addr: "127.0.0.1:8080", DataDir: "./data", LogLevel: slog.LevelInfo, MaxRequestBody: 128 << 20, PricingInterval: 6 * time.Hour, Gateway: gateway.DefaultPolicy(), InferenceHeaderTimeout: 5 * time.Minute}
	c.UpdateDir = getenv("SUBLANE_UPDATE_DIR")
	if err := c.loadRefresh(getenv); err != nil {
		return c, err
	}
	if c.UpdateDir != "" && !filepath.IsAbs(c.UpdateDir) {
		return c, fmt.Errorf("SUBLANE_UPDATE_DIR must be an absolute host-worker mailbox path")
	}
	if err := c.loadGateway(getenv); err != nil {
		return c, err
	}
	if err := c.loadBackup(getenv); err != nil {
		return c, err
	}
	if v := getenv("SUBLANE_DEMO"); v != "" {
		if v != "true" && v != "false" {
			return c, fmt.Errorf("SUBLANE_DEMO must be true or false")
		}
		c.Demo = v == "true"
	}
	if v := getenv("SUBLANE_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("SUBLANE_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := getenv("SUBLANE_CREDENTIALS_KEY_FILE"); v != "" {
		if !c.Demo {
			if _, err := vault.KeyPath(c.DataDir, v); err != nil {
				return c, fmt.Errorf("SUBLANE_CREDENTIALS_KEY_FILE: %w", err)
			}
		}
		c.CredentialsKeyFile = v
	}
	if v := getenv("SUBLANE_LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return c, fmt.Errorf("SUBLANE_LOG_LEVEL: %w", err)
		}
	}
	if v := getenv("SUBLANE_MAX_REQUEST_BODY_MB"); v != "" {
		mb, err := strconv.ParseInt(v, 10, 64)
		if err != nil || mb <= 0 || mb > math.MaxInt64>>20 {
			return c, fmt.Errorf("SUBLANE_MAX_REQUEST_BODY_MB must be a positive integer fitting in bytes")
		}
		c.MaxRequestBody = mb << 20
	}
	if v := getenv("SUBLANE_PUBLIC_URL"); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return c, fmt.Errorf("SUBLANE_PUBLIC_URL must be an http(s) origin without credentials, path, query, or fragment")
		}
		host := strings.ToLower(u.Host)
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return c, fmt.Errorf("invalid SUBLANE_PUBLIC_URL port")
			}
			if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
				host = strings.ToLower(u.Hostname())
				if strings.Contains(host, ":") {
					host = "[" + host + "]"
				}
			}
		}
		c.PublicURL = u.Scheme + "://" + host
	}
	if v := getenv("SUBLANE_TRUSTED_PROXIES"); v != "" {
		for _, part := range strings.Split(v, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
			if err != nil {
				return c, fmt.Errorf("SUBLANE_TRUSTED_PROXIES must be comma-separated IP CIDRs: %w", err)
			}
			c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
		}
	}
	c.PricingURL = getenv("SUBLANE_PRICING_URL")
	c.MetricsToken = getenv("SUBLANE_METRICS_TOKEN")
	if c.MetricsToken != "" {
		if len(c.MetricsToken) < 32 || len(c.MetricsToken) > 128 {
			return c, fmt.Errorf("SUBLANE_METRICS_TOKEN must contain 32–128 visible ASCII characters")
		}
		for _, char := range c.MetricsToken {
			if char < 33 || char > 126 {
				return c, fmt.Errorf("SUBLANE_METRICS_TOKEN must contain 32–128 visible ASCII characters")
			}
		}
	}
	c.PricingHashURL = getenv("SUBLANE_PRICING_HASH_URL")
	c.PricingCache = getenv("SUBLANE_PRICING_CACHE")
	c.PricingFallback = getenv("SUBLANE_PRICING_FALLBACK")
	c.PricingOverride = getenv("SUBLANE_PRICING_OVERRIDE")
	if v := getenv("SUBLANE_PRICING_INTERVAL"); v != "" {
		interval, err := time.ParseDuration(v)
		if err != nil || interval <= 0 {
			return c, fmt.Errorf("SUBLANE_PRICING_INTERVAL must be a positive duration")
		}
		c.PricingInterval = interval
	}
	_, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return c, fmt.Errorf("SUBLANE_ADDR must be host:port: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("SUBLANE_ADDR port must be between 1 and 65535")
	}
	return c, nil
}
