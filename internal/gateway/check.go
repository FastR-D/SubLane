package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/upstream"
)

type ModelCheck struct {
	Model      string `json:"model"`
	Available  bool   `json:"available"`
	DurationMS int64  `json:"duration_ms"`
	ErrorCode  string `json:"error_code"`
}

// CheckModel tests one execution resource, independently of pools and member allowances.
func (s *Service) CheckModel(parent context.Context, id, model string) (ModelCheck, error) {
	if len(model) > 128 || !safeModel.MatchString(model) {
		return ModelCheck{}, upstream.ErrInput
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.runContext, cancel)
	defer stop()
	err := s.checkModel(ctx, id, model)
	if errors.Is(err, accounts.ErrNotFound) || errors.Is(err, ErrBusy) {
		return ModelCheck{}, err
	}
	result := ModelCheck{Model: model, Available: err == nil, DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		_, result.ErrorCode, _ = classify(ctx, err)
		if errors.Is(err, accounts.ErrProviderDisabled) {
			result.ErrorCode = "provider_disabled"
		}
	}
	return result, nil
}

func (s *Service) checkModel(ctx context.Context, id, model string) error {
	raw, _ := json.Marshal(map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 16})
	if s.content != nil {
		if _, err := s.content.Check(ctx, raw, s.MaxRequestBody(), model); err != nil {
			return err
		}
	}
	release, err := s.leaseModelCheck(ctx, id, model)
	if err != nil {
		return err
	}
	defer release()
	_, err = readAccount(ctx, s, id, func(ctx context.Context, credential accounts.Credential) (bool, error) {
		stream, err := s.provider.Responses(ctx, credential, raw, nil, false)
		if err != nil {
			return false, err
		}
		defer stream.Body.Close()
		if stream.StatusCode < 200 || stream.StatusCode >= 300 {
			return false, &upstream.UpstreamError{Status: stream.StatusCode, RetryAfter: stream.Header.Get("Retry-After")}
		}
		stream.Body = struct {
			io.Reader
			io.Closer
		}{io.LimitReader(stream.Body, 64<<10), stream.Body}
		output := false
		err = stream.Events(func(raw []byte) error {
			var event struct {
				Type, Delta string
				Response    struct {
					Output []struct {
						Content []struct{ Text, Refusal string }
					}
				}
			}
			if json.Unmarshal(raw, &event) != nil {
				return upstream.ErrResponse
			}
			if event.Type == "response.output_text.delta" && event.Delta != "" {
				output = true
			}
			for _, item := range event.Response.Output {
				for _, content := range item.Content {
					output = output || content.Text != "" || content.Refusal != ""
				}
			}
			return nil
		})
		// A successful HTTP status or a partial delta alone does not prove generation completed.
		if err == nil && !output {
			err = upstream.ErrResponse
		}
		return err == nil, err
	})
	return err
}

func (s *Service) leaseModelCheck(ctx context.Context, id, model string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.accounts.ProviderEnabled(account.Provider) {
		return nil, accounts.ErrProviderDisabled
	}
	if err := s.loadRuntime(ctx); err != nil {
		return nil, err
	}
	if err := s.accountAdmission(account); err != nil {
		return nil, err
	}
	known, supported, err := s.catalogSupport(ctx, s.queries, id, model, account.Provider)
	if err != nil {
		return nil, err
	}
	if !known {
		return nil, ErrCatalogUnavailable
	}
	if !supported {
		return nil, ErrModelUnavailable
	}
	if err := s.modelAdmission(ctx, s.queries, id, model); err != nil {
		return nil, err
	}
	if err := s.quotaAdmission(ctx, s.queries, account, model); err != nil {
		return nil, err
	}
	modelState, err := s.modelLease(ctx, id, model)
	if err != nil {
		return nil, err
	}
	releaseSlot, err := s.Acquire()
	if err != nil {
		return nil, err
	}
	state := s.health[id]
	state.InFlight++
	modelState.inFlight++
	s.modelHealth[modelState.key] = modelState
	s.workers.Add(1)
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Management tests consume capacity without changing routing cooldowns or member accounting.
		state.InFlight--
		modelState.inFlight--
		if modelState.inFlight == 0 && !modelState.persistFailed && s.modelHealth[modelState.key] == modelState {
			delete(s.modelHealth, modelState.key)
		}
		releaseSlot()
		s.workers.Done()
	}, nil
}
