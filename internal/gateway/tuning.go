package gateway

import (
	"fmt"
	"time"
)

// Policy is immutable after startup; each workspace receives its own capacity and queue.
type Policy struct {
	QuotaWeighted                                                      bool
	Capacity, QueueLimit, Retries                                      int
	QueueTimeout, RequestTimeout, StreamIdleTimeout, KeepaliveInterval time.Duration
	UploadIdleTimeout, UploadTimeout                                   time.Duration
	RetryDelay, TransientWindow, TransientCooldown, RateLimitCooldown  time.Duration
	TransientThreshold                                                 int64
}

func DefaultPolicy() Policy {
	return Policy{Capacity: 128, QueueLimit: 100, QueueTimeout: 30 * time.Second,
		RequestTimeout: 30 * time.Minute, KeepaliveInterval: 10 * time.Second,
		UploadIdleTimeout: 2 * time.Minute, UploadTimeout: 15 * time.Minute,
		Retries: 2, RetryDelay: 500 * time.Millisecond, TransientThreshold: 3,
		TransientWindow: 2 * time.Minute, TransientCooldown: 10 * time.Second, RateLimitCooldown: 5 * time.Second}
}

func (p Policy) Validate() error {
	if p.Capacity < 1 || p.Capacity > 1000 || p.QueueLimit < 0 || p.QueueLimit > 1000 || p.Retries < 0 || p.Retries > 3 || p.TransientThreshold < 0 || p.TransientThreshold > 16 {
		return fmt.Errorf("gateway capacity must be 1–1000, queue limit 0–1000, retries 0–3, and transient threshold 0–16")
	}
	for name, value := range map[string]time.Duration{"queue timeout": p.QueueTimeout, "request timeout": p.RequestTimeout, "upload idle timeout": p.UploadIdleTimeout, "upload timeout": p.UploadTimeout, "retry delay": p.RetryDelay, "transient window": p.TransientWindow, "transient cooldown": p.TransientCooldown, "rate limit cooldown": p.RateLimitCooldown} {
		if value <= 0 || value > 24*time.Hour {
			return fmt.Errorf("gateway %s must be positive and at most 24h", name)
		}
	}
	if p.QueueTimeout > 10*time.Minute || p.RetryDelay > 5*time.Second || p.TransientCooldown > 5*time.Minute || p.RateLimitCooldown > time.Hour {
		return fmt.Errorf("gateway queue timeout, retry delay, transient cooldown or rate limit cooldown exceeds its bound")
	}
	if p.StreamIdleTimeout < 0 || p.StreamIdleTimeout > 24*time.Hour || p.KeepaliveInterval < 0 || p.KeepaliveInterval > time.Minute {
		return fmt.Errorf("gateway stream idle timeout must be 0–24h and keepalive interval 0–1m")
	}
	return nil
}

func (s *Service) Policy() Policy { return s.policy }
