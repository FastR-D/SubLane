package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/murongg/SubLane/internal/storage/db"
)

type RefreshError struct {
	RetryAfter int64
}

func (e *RefreshError) Error() string { return ErrRefresh.Error() }
func (e *RefreshError) Unwrap() error { return ErrRefresh }

type refreshState struct {
	failures int
	retryAt  time.Time
	fallback bool
}

func (s *Service) Prepare(ctx context.Context, id string, refresh func(context.Context, Credential) (Credential, error)) (Credential, error) {
	return s.prepare(ctx, id, "", "request", refresh)
}

// RefreshDue also maintains disabled Codex credentials without enabling them or exposing credentials.
// It shares the same durable owner as request-triggered refresh.
func (s *Service) RefreshDue(ctx context.Context, id string, refresh func(context.Context, Credential) (Credential, error)) error {
	_, err := s.prepare(ctx, id, "", "scheduled", refresh)
	return err
}

func (s *Service) RefreshAfterRejection(ctx context.Context, id, rejectedToken string, refresh func(context.Context, Credential) (Credential, error)) (Credential, error) {
	if rejectedToken == "" {
		return Credential{}, ErrInput
	}
	return s.prepare(ctx, id, rejectedToken, "unauthorized", refresh)
}

func (s *Service) prepare(ctx context.Context, id, rejectedToken, trigger string, refresh func(context.Context, Credential) (Credential, error)) (Credential, error) {
	unlock, err := s.lockCredential(ctx, id)
	if err != nil {
		return Credential{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if err := s.rotationError(); err != nil {
		return Credential{}, err
	}
	row, err := s.get(ctx, id)
	if err != nil {
		return Credential{}, err
	}
	if !s.ProviderEnabled(row.Provider) {
		return Credential{}, ErrProviderDisabled
	}
	// Disabling an account blocks execution, while scheduled Codex maintenance preserves its credentials.
	if !row.Enabled && (trigger != "scheduled" || row.Provider != "codex") {
		return Credential{}, ErrDisabled
	}
	if row.Status == "reauth_required" {
		return Credential{}, ErrReauthorize
	}
	credential, err := s.decrypt(row)
	if err != nil {
		return Credential{}, err
	}
	credential, changed, err := prepareClaudeIdentity(credential, "")
	if err != nil {
		return Credential{}, err
	}
	// Legacy credentials must acquire a durable device before any provider IO.
	if changed {
		if err := s.persist(ctx, s.queries, id, credential, row.Status); err != nil {
			return Credential{}, err
		}
	}
	// A late 401 must not reject a token another request has already rotated.
	if credential.AccessToken == rejectedToken && !credential.Rejected {
		credential.Rejected = true
		if err := s.persist(ctx, s.queries, id, credential, row.Status); err != nil {
			return Credential{}, err
		}
	}
	if row.ProxyID != nil {
		credential.ProxyURL, err = s.proxyURL(ctx, *row.ProxyID)
		if err != nil {
			return Credential{}, err
		}
	}
	if credential.Kind() == "openai" {
		// Static keys have no expiry or OAuth refresh. Rejection remains durable until an administrator rotates the key.
		if credential.Rejected {
			if err := s.queries.SetAccountStatus(ctx, db.SetAccountStatusParams{ID: id, Status: "reauth_required", UpdatedAt: s.now().Unix()}); err != nil {
				return Credential{}, err
			}
			return Credential{}, ErrReauthorize
		}
		return credential, nil
	}
	minimumValidity := 2 * time.Minute
	needsProject := false
	if credential.Kind() == "antigravity" {
		// Its SDK may refresh within five minutes of expiry; reserve the ten-minute request budget too.
		minimumValidity = 16 * time.Minute
		var project string
		_ = json.Unmarshal(credential.Metadata["project_id"], &project)
		needsProject = project == ""
	}
	if !needsProject && !credential.Rejected && credential.ExpiresAt > s.now().Add(minimumValidity).Unix() {
		lead := minimumValidity
		if credential.Kind() == "codex" {
			lead = s.codexRefreshLead()
			if credential.RefreshAfter > s.now().Unix() {
				return credential, nil
			}
		}
		if credential.ExpiresAt > s.now().Add(lead).Unix() {
			return credential, nil
		}
	}
	state := s.refreshState(id)
	if s.now().Before(state.retryAt) {
		return s.refreshFallback(credential, state)
	}
	if refresh == nil {
		return Credential{}, ErrRefresh
	}
	finish, err := s.beginRotation()
	if err != nil {
		return Credential{}, err
	}
	defer finish()
	// Refresh and administrator writes share this owner. Publish rotated credentials only after persistence.
	// Once rotation starts, a disconnected caller must not discard a consumed refresh token's replacement.
	// Keep provider IO and persistence synchronous and bounded under the same independent context.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	deviceID := claudeDeviceID(credential.Metadata)
	updated, err := refresh(refreshCtx, credential)
	if err != nil {
		code, status := refreshFailureMetadata(err)
		s.logRefresh(refreshCtx, id, credential, trigger, "failed", code, status)
		if errors.Is(err, ErrReauthorize) {
			if saveErr := s.queries.SetAccountStatus(refreshCtx, db.SetAccountStatusParams{ID: id, Status: "reauth_required", UpdatedAt: s.now().Unix()}); saveErr != nil {
				s.logRefresh(refreshCtx, id, credential, trigger, "persist_failed", "credential_persistence_failed", 0)
				return Credential{}, saveErr
			}
			s.clearRefresh(id)
			return Credential{}, ErrReauthorize
		}
		if ctx.Err() != nil {
			return Credential{}, ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return Credential{}, err
		}
		return s.refreshFallback(credential, s.deferRefresh(id, err, true))
	}
	updated, _, err = prepareClaudeIdentity(updated, deviceID)
	if err != nil {
		s.logRefresh(refreshCtx, id, credential, trigger, "invalid_result", "invalid_identity", 0)
		s.deferRefresh(id, err, false)
		return Credential{}, err
	}
	if err := validateRefresh(credential, updated, s.now().Add(minimumValidity).Unix()); err != nil {
		s.logRefresh(refreshCtx, id, credential, trigger, "invalid_result", "invalid_refresh_result", 0)
		s.deferRefresh(id, err, false)
		return Credential{}, err
	}
	updated.Rejected = false
	if updated.Kind() == "codex" {
		// Short-lived rotations must not immediately re-enter a lead intended for ten-day tokens.
		validity := time.Duration(updated.ExpiresAt-s.now().Unix()) * time.Second
		updated.RefreshAfter = updated.ExpiresAt - int64(min(s.codexRefreshLead(), validity/2)/time.Second)
	}
	if err := s.persist(refreshCtx, s.queries, id, updated, "ready"); err != nil {
		s.logRefresh(refreshCtx, id, credential, trigger, "persist_failed", "credential_persistence_failed", 0)
		s.deferRefresh(id, err, false)
		return Credential{}, err
	}
	if credential.Rejected && state.failures > 0 {
		// A token endpoint success alone must not reset consecutive business-401 backoff.
		state.retryAt, state.fallback = time.Time{}, false
		s.refreshMu.Lock()
		s.refreshes[id] = state
		s.refreshMu.Unlock()
	} else {
		s.clearRefresh(id)
	}
	s.logRefresh(refreshCtx, id, updated, trigger, "success", "", 0)
	if ctx.Err() != nil {
		return Credential{}, ctx.Err()
	}
	updated.ProxyURL = credential.ProxyURL
	return updated, nil
}

func validateRefresh(previous, updated Credential, minimumExpiry int64) error {
	if !sameIdentity(previous, updated) {
		return ErrIdentity
	}
	if err := updated.validate(); err != nil {
		return err
	}
	if updated.ExpiresAt <= minimumExpiry || (previous.Rejected && updated.AccessToken == previous.AccessToken) {
		return ErrRefresh
	}
	return nil
}

func (s *Service) deferRefresh(id string, err error, fallback bool) refreshState {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	state := s.refreshes[id]
	delay := min(5*time.Minute, (30*time.Second)<<state.failures)
	var unavailable *RefreshError
	if errors.As(err, &unavailable) {
		delay = max(delay, time.Duration(min(3600, max(0, unavailable.RetryAfter)))*time.Second)
	}
	state.failures = min(4, state.failures+1)
	state.retryAt = s.now().Add(delay)
	// A returned rotation that fails validation or persistence must not turn into old-token fallback on the next read.
	state.fallback = fallback
	if s.refreshes == nil {
		s.refreshes = make(map[string]refreshState)
	}
	s.refreshes[id] = state
	return state
}

func (s *Service) refreshFallback(credential Credential, state refreshState) (Credential, error) {
	now := s.now()
	// Recheck after network IO: the early-refresh window is not the token's actual expiry.
	// Other providers retain their SDK validity requirements and cannot use this fallback.
	if state.fallback && credential.Kind() == "codex" && !credential.Rejected && credential.ExpiresAt > now.Add(30*time.Second).Unix() {
		return credential, nil
	}
	return Credential{}, &RefreshError{RetryAfter: max(1, int64((state.retryAt.Sub(now)+time.Second-1)/time.Second))}
}
