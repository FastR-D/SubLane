package accounts

import (
	"context"
	"errors"
	"log/slog"

	"github.com/murongg/SubLane/internal/logging"
)

// RefreshFailure carries only normalized provider metadata, never a provider response body.
type RefreshFailure struct {
	Status int
	Code   string
	Err    error
}

func (e *RefreshFailure) Error() string { return e.Err.Error() }
func (e *RefreshFailure) Unwrap() error { return e.Err }

func refreshFailureMetadata(err error) (string, int) {
	var failure *RefreshFailure
	if errors.As(err, &failure) {
		code := RefreshFailureCode(failure.Code)
		status := failure.Status
		if status < 100 || status > 599 {
			status = 0
		}
		return code, status
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled", 0
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", 0
	case errors.Is(err, ErrReauthorize):
		return "reauthorization_required", 0
	case errors.Is(err, ErrRefresh):
		return "temporarily_unavailable", 0
	default:
		return "refresh_failed", 0
	}
}

// RefreshFailureCode bounds diagnostics at the provider boundary as well as at logging.
func RefreshFailureCode(code string) string {
	switch code {
	case "invalid_grant", "invalid_refresh_token", "refresh_token_reused", "refresh_token_invalidated", "refresh_token_expired", "token_expired", "app_session_terminated", "invalid_client", "invalid_scope", "temporarily_unavailable", "rate_limited", "upstream_unavailable":
		return code
	default:
		return "unknown"
	}
}

func (s *Service) logRefresh(ctx context.Context, id string, c Credential, trigger, outcome, code string, status int) {
	level := slog.LevelWarn
	if outcome == "success" {
		level = slog.LevelInfo
	}
	logging.From(ctx).Log(ctx, level, "Account credential refresh",
		"tenant_id", s.tenantID, "account_id", id, "provider", c.Kind(),
		"trigger", trigger, "outcome", outcome, "error_code", code,
		"upstream_status", status, "remaining_seconds", max(-366*86400, min(366*86400, c.ExpiresAt-s.now().Unix())))
}
