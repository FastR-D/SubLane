package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/murongg/SubLane/internal/upstream"
)

const canceledStreamDrain = 3 * time.Second

// Exchange owns the account lease until body closure, including cancellation and partial streams.
type Exchange struct {
	*upstream.Stream
	mu                            sync.Mutex
	closeOnce                     sync.Once
	raw                           io.ReadCloser
	stop                          func() bool
	releaseUpstream               func()
	entry                         *observation
	closed                        bool
	outcome, code, penalty, retry string
}
type exchangeBody struct{ x *Exchange }

func (b *exchangeBody) Read(p []byte) (int, error) { return b.x.raw.Read(p) }
func (b *exchangeBody) Close() error               { return b.x.close() }
func trackExchange(stream *upstream.Stream, entry *observation, upstreamCtx context.Context, releaseUpstream func()) *Exchange {
	x := &Exchange{Stream: stream, entry: entry, raw: stream.Body, releaseUpstream: releaseUpstream, outcome: "canceled", code: "client_disconnected"}
	status := int64(stream.StatusCode)
	entry.record.UpstreamStatus = &status
	if entry.record.Provider == "codex" && status >= 200 && status < 300 {
		if value, ok := upstream.QuotaHeaders(stream.Header, entry.service.now()); ok {
			value.ReadStartedAt = entry.quotaReadStartedAt
			entry.quota = &value
		}
	}
	if status < 200 || status >= 300 {
		x.outcome, x.code, x.penalty = statusOutcome(int(status))
		x.retry = stream.Header.Get("Retry-After")
	}
	stream.Body = &exchangeBody{x: x}
	// A detached Codex stream remains readable until its bounded upstream context ends.
	closeCtx := entry.ctx
	if releaseUpstream != nil {
		closeCtx = upstreamCtx
	}
	// Install the callback under the same mutex it acquires; canceled contexts may run it immediately.
	x.mu.Lock()
	x.stop = context.AfterFunc(closeCtx, func() { x.close() })
	x.mu.Unlock()
	return x
}
func (x *Exchange) close() error {
	var err error
	x.closeOnce.Do(func() {
		if x.releaseUpstream != nil {
			x.releaseUpstream()
		}
		err = x.raw.Close()
		x.mu.Lock()
		defer x.mu.Unlock()
		x.closed = true
		if x.stop != nil {
			x.stop()
		}
		if x.outcome == "canceled" && x.entry.ctx.Err() != nil {
			x.outcome, x.code, x.penalty = classify(x.entry.ctx, x.entry.ctx.Err())
		}
		x.entry.finish(x.outcome, x.code, x.penalty, x.retry)
	})
	return err
}
func (x *Exchange) Events(yield func([]byte) error) error {
	terminal := ""
	consumerError := false
	var downstreamErr error
	err := x.Stream.Events(func(raw []byte) error {
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(raw, &event)
		x.mu.Lock()
		if !x.closed {
			if x.entry.record.Provider == "codex" && event.Type == "codex.rate_limits" {
				if value, ok := upstream.QuotaEvent(raw, x.entry.service.now()); ok {
					// Stream events can describe pre-request usage. Never advance the
					// accounting read boundary to completion just because the frame arrived late.
					value.ReadStartedAt = x.entry.quotaReadStartedAt
					x.entry.quota = &value
				}
			}
			if x.entry.kind == Messages {
				var message struct {
					Type  string `json:"type"`
					Delta struct {
						Text, Thinking string
						PartialJSON    string `json:"partial_json"`
						StopReason     string `json:"stop_reason"`
					} `json:"delta"`
				}
				_ = json.Unmarshal(raw, &message)
				if message.Type == "content_block_delta" && (message.Delta.Text != "" || message.Delta.Thinking != "" || message.Delta.PartialJSON != "") && x.entry.record.FirstTokenMs == nil {
					elapsed := max(0, x.entry.service.now().Sub(x.entry.started).Milliseconds())
					x.entry.record.FirstTokenMs = &elapsed
				}
				x.entry.record.InputTokens, x.entry.record.OutputTokens, x.entry.record.CachedTokens = x.Stream.MessageUsage(raw)
				if message.Delta.StopReason == "max_tokens" {
					terminal = "response.incomplete"
				}
			} else if x.entry.kind.IsGemini() {
				output, incomplete := upstream.GeminiEventInfo(raw)
				if output && x.entry.record.FirstTokenMs == nil {
					elapsed := max(0, x.entry.service.now().Sub(x.entry.started).Milliseconds())
					x.entry.record.FirstTokenMs = &elapsed
				}
				x.entry.record.InputTokens, x.entry.record.OutputTokens, x.entry.record.CachedTokens = x.Stream.GeminiUsage(raw)
				if incomplete {
					terminal = "response.incomplete"
				}
			} else {
				// Lifecycle events and empty deltas are not generated output. Missing observations
				// stay null, including compact or buffered responses without a measurable delta.
				if event.Delta != "" && x.entry.record.FirstTokenMs == nil {
					switch event.Type {
					case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta":
						elapsed := max(0, x.entry.service.now().Sub(x.entry.started).Milliseconds())
						x.entry.record.FirstTokenMs = &elapsed
					}
				}
				if event.Type == "response.completed" || event.Type == "response.incomplete" || event.Type == "response.failed" {
					terminal = event.Type
					x.entry.observe(raw)
					x.entry.usageFinal = x.entry.record.InputTokens != nil && x.entry.record.OutputTokens != nil
				}
			}
		}
		x.mu.Unlock()
		if event.Type == "response.failed" || downstreamErr != nil {
			return nil
		}
		err := yield(raw)
		if err != nil {
			consumerError = true
			if x.releaseUpstream != nil && x.entry.ctx.Err() != nil {
				downstreamErr = err
				return nil
			}
		}
		return err
	})
	if downstreamErr != nil {
		err = downstreamErr
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.closed {
		if err == nil {
			x.outcome = "success"
			if terminal == "response.incomplete" {
				x.outcome = "incomplete"
			}
			x.code = ""
			x.penalty = ""
		} else if consumerError {
			if x.entry.ctx.Err() != nil {
				x.outcome, x.code, x.penalty = classify(x.entry.ctx, x.entry.ctx.Err())
			} else if errors.Is(err, ErrContextLimit) {
				x.outcome, x.code, x.penalty = "rejected", "context_limit", ""
			} else {
				x.outcome, x.code, x.penalty = "canceled", "client_disconnected", ""
			}
		} else {
			x.outcome, x.code, x.penalty = classify(x.entry.ctx, err)
			x.entry.streamFailure = explicitStreamFailure(err)
		}
	}
	return err
}

func (x *Exchange) AcceptMessage(raw []byte) error {
	if err := upstream.ValidateMessageResponse(raw); err != nil {
		x.Fail(err)
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.closed {
		x.entry.record.InputTokens, x.entry.record.OutputTokens, x.entry.record.CachedTokens = x.Stream.MessageUsage(raw)
		var message struct {
			StopReason string `json:"stop_reason"`
		}
		_ = json.Unmarshal(raw, &message)
		x.outcome, x.code, x.penalty = "success", "", ""
		if message.StopReason == "max_tokens" {
			x.outcome = "incomplete"
		}
	}
	return nil
}

func (x *Exchange) AcceptGemini(raw []byte) error {
	if err := upstream.ValidateGeminiResponse(raw); err != nil {
		x.Fail(err)
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.closed {
		x.entry.record.InputTokens, x.entry.record.OutputTokens, x.entry.record.CachedTokens = x.Stream.GeminiUsage(raw)
		x.outcome, x.code, x.penalty = "success", "", ""
		if _, incomplete := upstream.GeminiEventInfo(raw); incomplete {
			x.outcome = "incomplete"
		}
	}
	return nil
}
func (x *Exchange) Fail(err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.closed {
		x.outcome, x.code, x.penalty = classify(x.entry.ctx, err)
		x.entry.streamFailure = explicitStreamFailure(err)
	}
}

func explicitStreamFailure(err error) bool {
	var failure *upstream.UpstreamError
	return errors.Is(err, upstream.ErrInterrupted) || errors.Is(err, upstream.ErrResponse) ||
		errors.As(err, &failure) && failure.Status >= 500
}
func (x *Exchange) Complete(ctx context.Context, event []byte) []byte {
	data := x.Stream.Complete(ctx, event)
	if !json.Valid(data) {
		x.Fail(upstream.ErrResponse)
	}
	return data
}
func (x *Exchange) AcceptCompact(raw []byte) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.closed {
		x.entry.observe(raw)
		x.outcome = "success"
		x.code = ""
		x.penalty = ""
	}
}
func statusOutcome(status int) (string, string, string) {
	switch {
	case status == 429:
		return "error", "rate_limited", "rate_limited"
	case status == 401:
		return "error", "auth_required", ""
	case status == 403:
		return "error", "upstream_forbidden", ""
	case status == 408:
		return "error", "timeout", "timeout"
	case status >= 500:
		return "error", "upstream_unavailable", "upstream_error"
	default:
		return "rejected", "upstream_rejected", ""
	}
}
