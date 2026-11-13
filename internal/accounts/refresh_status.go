package accounts

// CredentialRefreshStatus exposes only current process observations, never credentials or provider errors.
// ServerTime lets the browser compare expiry and retry times without trusting its wall clock.
type CredentialRefreshStatus struct {
	ServerTime    int64 `json:"server_time"`
	Refreshing    bool  `json:"refreshing"`
	Failed        bool  `json:"failed"`
	LastFailureAt int64 `json:"last_failure_at"`
	RetryAt       int64 `json:"retry_at"`
}

func (s *Service) withRefreshStatus(account Account) Account {
	if account.Provider != "codex" {
		return account
	}
	state := s.refreshState(account.ID)
	status := &CredentialRefreshStatus{ServerTime: s.now().Unix(), Refreshing: state.refreshing, Failed: state.lastFailureAt != 0, LastFailureAt: state.lastFailureAt}
	if !state.retryAt.IsZero() {
		status.RetryAt = state.retryAt.Unix()
	}
	account.CredentialRefresh = status
	return account
}

func (s *Service) startRefresh(id string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	state := s.refreshes[id]
	state.refreshing = true
	if s.refreshes == nil {
		s.refreshes = make(map[string]refreshState)
	}
	s.refreshes[id] = state
}

func (s *Service) finishRefresh(id string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if state, ok := s.refreshes[id]; ok {
		state.refreshing = false
		s.refreshes[id] = state
	}
}
