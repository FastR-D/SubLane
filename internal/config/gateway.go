package config

import (
	"fmt"
	"strconv"
	"time"
)

func (c *Config) loadGateway(getenv func(string) string) error {
	p := &c.Gateway
	if value := getenv("SUBLANE_GATEWAY_QUOTA_WEIGHTED"); value != "" {
		if value != "true" && value != "false" {
			return fmt.Errorf("SUBLANE_GATEWAY_QUOTA_WEIGHTED must be true or false")
		}
		p.QuotaWeighted = value == "true"
	}
	for key, target := range map[string]*int{
		"SUBLANE_GATEWAY_CAPACITY": &p.Capacity, "SUBLANE_GATEWAY_QUEUE_LIMIT": &p.QueueLimit, "SUBLANE_GATEWAY_RETRIES": &p.Retries,
	} {
		if value := getenv(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s must be an integer", key)
			}
			*target = n
		}
	}
	if value := getenv("SUBLANE_GATEWAY_TRANSIENT_THRESHOLD"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("SUBLANE_GATEWAY_TRANSIENT_THRESHOLD must be an integer")
		}
		p.TransientThreshold = n
	}
	for key, target := range map[string]*time.Duration{
		"SUBLANE_GATEWAY_QUEUE_TIMEOUT": &p.QueueTimeout, "SUBLANE_GATEWAY_REQUEST_TIMEOUT": &p.RequestTimeout,
		"SUBLANE_GATEWAY_UPLOAD_IDLE_TIMEOUT": &p.UploadIdleTimeout, "SUBLANE_GATEWAY_UPLOAD_TIMEOUT": &p.UploadTimeout,
		"SUBLANE_GATEWAY_STREAM_IDLE_TIMEOUT": &p.StreamIdleTimeout, "SUBLANE_GATEWAY_KEEPALIVE_INTERVAL": &p.KeepaliveInterval,
		"SUBLANE_GATEWAY_RETRY_DELAY": &p.RetryDelay, "SUBLANE_GATEWAY_TRANSIENT_WINDOW": &p.TransientWindow,
		"SUBLANE_GATEWAY_TRANSIENT_COOLDOWN": &p.TransientCooldown, "SUBLANE_GATEWAY_RATE_LIMIT_COOLDOWN": &p.RateLimitCooldown,
		"SUBLANE_INFERENCE_HEADER_TIMEOUT": &c.InferenceHeaderTimeout,
	} {
		if value := getenv(key); value != "" {
			d, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s must be a duration", key)
			}
			*target = d
		}
	}
	if c.InferenceHeaderTimeout < 0 || c.InferenceHeaderTimeout > 24*time.Hour {
		return fmt.Errorf("SUBLANE_INFERENCE_HEADER_TIMEOUT must be 0–24h")
	}
	return p.Validate()
}
