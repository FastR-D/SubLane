package config

import (
	"testing"
	"time"
)

func TestGatewayPolicyOverridesAndBounds(t *testing.T) {
	env := map[string]string{
		"SUBLANE_GATEWAY_CAPACITY": "64", "SUBLANE_GATEWAY_QUEUE_LIMIT": "40",
		"SUBLANE_GATEWAY_QUEUE_TIMEOUT": "45s", "SUBLANE_GATEWAY_REQUEST_TIMEOUT": "20m",
		"SUBLANE_GATEWAY_STREAM_IDLE_TIMEOUT": "3m", "SUBLANE_GATEWAY_KEEPALIVE_INTERVAL": "5s",
		"SUBLANE_GATEWAY_RETRIES": "1", "SUBLANE_GATEWAY_RETRY_DELAY": "250ms",
		"SUBLANE_GATEWAY_TRANSIENT_THRESHOLD": "5", "SUBLANE_GATEWAY_TRANSIENT_WINDOW": "1m",
		"SUBLANE_GATEWAY_TRANSIENT_COOLDOWN": "15s", "SUBLANE_GATEWAY_RATE_LIMIT_COOLDOWN": "8s",
		"SUBLANE_INFERENCE_HEADER_TIMEOUT":    "0s",
		"SUBLANE_GATEWAY_UPLOAD_IDLE_TIMEOUT": "90s", "SUBLANE_GATEWAY_UPLOAD_TIMEOUT": "12m",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.Capacity != 64 || cfg.Gateway.QueueLimit != 40 || cfg.Gateway.QueueTimeout != 45*time.Second || cfg.Gateway.Retries != 1 || cfg.Gateway.RequestTimeout != 20*time.Minute || cfg.Gateway.StreamIdleTimeout != 3*time.Minute || cfg.InferenceHeaderTimeout != 0 {
		t.Fatalf("policy not applied: %+v", cfg)
	}
	if cfg.Gateway.UploadIdleTimeout != 90*time.Second || cfg.Gateway.UploadTimeout != 12*time.Minute {
		t.Fatalf("upload policy not applied: %+v", cfg.Gateway)
	}
	for key, value := range map[string]string{
		"SUBLANE_GATEWAY_CAPACITY": "0", "SUBLANE_GATEWAY_QUEUE_LIMIT": "-1", "SUBLANE_GATEWAY_RETRIES": "20",
		"SUBLANE_GATEWAY_REQUEST_TIMEOUT": "0s", "SUBLANE_GATEWAY_QUEUE_TIMEOUT": "invalid", "SUBLANE_GATEWAY_TRANSIENT_WINDOW": "-1s",
		"SUBLANE_INFERENCE_HEADER_TIMEOUT":    "-1s",
		"SUBLANE_GATEWAY_UPLOAD_IDLE_TIMEOUT": "0s", "SUBLANE_GATEWAY_UPLOAD_TIMEOUT": "25h",
	} {
		if _, err := Load(func(k string) string {
			if k == key {
				return value
			}
			return ""
		}); err == nil {
			t.Fatalf("invalid %s=%s accepted", key, value)
		}
	}
}

func TestDefaultUploadBudgets(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil || cfg.Gateway.UploadIdleTimeout != 2*time.Minute || cfg.Gateway.UploadTimeout != 15*time.Minute {
		t.Fatalf("unexpected upload defaults: %+v, %v", cfg.Gateway, err)
	}
}
