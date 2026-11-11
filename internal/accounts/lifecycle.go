package accounts

import (
	"context"
	"time"
)

const CodexRefreshLead = 96 * time.Hour

type credentialLock struct {
	gate  chan struct{}
	users int
}

// ConfigureRefresh installs the process policy before requests are accepted.
// Embedded callers that do not configure it retain the legacy two-minute lead.
func (s *Service) ConfigureRefresh(ctx context.Context, lead time.Duration) {
	s.processCtx, s.refreshLead = ctx, lead
}

func (s *Service) codexRefreshLead() time.Duration {
	if s.refreshLead > 0 {
		return s.refreshLead
	}
	return 2 * time.Minute
}

func (s *Service) lockCredential(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.refreshMu.Lock()
	if s.locks == nil {
		s.locks = make(map[string]*credentialLock)
	}
	lock := s.locks[id]
	if lock == nil {
		lock = &credentialLock{gate: make(chan struct{}, 1)}
		lock.gate <- struct{}{}
		s.locks[id] = lock
	}
	lock.users++
	s.refreshMu.Unlock()
	drop := func() {
		s.refreshMu.Lock()
		lock.users--
		if lock.users == 0 {
			delete(s.locks, id)
		}
		s.refreshMu.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-lock.gate:
		return func() { lock.gate <- struct{}{}; drop() }, nil
	}
}

func (s *Service) beginRotation() (func(), error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if err := s.rotationErrorLocked(); err != nil {
		return nil, err
	}
	s.rotations.Add(1)
	return s.rotations.Done, nil
}

func (s *Service) rotationError() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.rotationErrorLocked()
}

func (s *Service) rotationErrorLocked() error {
	if s.closing {
		return context.Canceled
	}
	if s.processCtx != nil && s.processCtx.Err() != nil {
		return s.processCtx.Err()
	}
	return nil
}

// Close stops admission before waiting, so SQLite can close only after every rotation is saved.
func (s *Service) Close() {
	s.lifecycleMu.Lock()
	s.closing = true
	s.lifecycleMu.Unlock()
	s.rotations.Wait()
}

func (s *Service) clearRefresh(id string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	delete(s.refreshes, id)
}

func (s *Service) refreshState(id string) refreshState {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshes[id]
}
