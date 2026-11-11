package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/murongg/SubLane/internal/logging"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestUpstreamFailureDiagnostics(t *testing.T) {
	const private = "synthetic-private-body sk-synthetic-secret https://user:password@example.test/private?token=synthetic-token"
	for _, tc := range []struct {
		name, code, kind, stage string
		operation               Kind
		status, errorStatus     int
		network                 bool
	}{
		{"Responses failed after 200", "server_error", "http_error", "stream", Responses, 200, 502, false},
		{"Messages failed after 200", "server_error", "http_error", "stream", Messages, 200, 502, false},
		{"HTTP rejection", "permission_denied", "http_error", "execute", Responses, 403, 403, false},
		{"Compact HTTP rejection", "permission_denied", "http_error", "execute", Compact, 403, 403, false},
		{"Unknown provider code", "unknown", "http_error", "stream", Messages, 200, 502, false},
		{"DNS failure", "", "dns_error", "execute", Responses, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			if code == "unknown" {
				code = "synthetic-private-code"
			}
			errorBody, err := json.Marshal(map[string]any{"error": map[string]string{"code": code, "type": "server_error", "message": private}})
			if err != nil {
				t.Fatal(err)
			}
			s, ids := codexGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				if tc.network {
					return nil, &url.Error{Op: "Post", URL: "https://user:password@example.test/?token=synthetic-token", Err: &net.DNSError{Name: "synthetic-private-host", Err: "synthetic-private-dns"}}
				}
				body := string(errorBody)
				if tc.status == 200 {
					body = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_synthetic\",\"output\":[]}}\n\n" +
						"data: {\"type\":\"response.failed\",\"response\":" + body + "}\n\n"
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"req_synthetic_upstream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))
			s.policy.Retries = 0
			var buffer bytes.Buffer
			ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&buffer, nil)))
			createAdmissionKey(t, s, 42, 1, 1)
			ctx = WithRequestIdentity(ctx, 42, "http")
			request := []byte(`{"model":"synthetic-model","input":"synthetic-private-prompt"}`)
			if tc.operation == Messages {
				request = []byte(`{"model":"synthetic-model","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"synthetic-private-prompt"}]}`)
			}
			x, err := s.Open(ctx, 1, 1, request, nil, tc.operation)
			if err == nil {
				if x.StatusCode == 200 {
					err = x.Events(func([]byte) error { return nil })
					if err == nil {
						t.Fatal("synthetic failed stream succeeded")
					}
				}
				x.Body.Close()
			}
			var diagnostic map[string]any
			count := 0
			for _, record := range gatewayLogRecords(t, &buffer) {
				if record["msg"] == "Upstream request failed" {
					diagnostic = record
					count++
				}
			}
			if count != 1 {
				t.Fatalf("expected one upstream diagnostic, got %d: %s", count, buffer.String())
			}
			if diagnostic["request_id"] != RequestID(ctx) || diagnostic["provider"] != "codex" || diagnostic["account_id"] != ids["codex"] || diagnostic["stage"] != tc.stage || diagnostic["upstream_error_kind"] != tc.kind {
				t.Fatal("upstream diagnostic lost correlation or failure stage", diagnostic)
			}
			if tc.status != 0 && (diagnostic["upstream_status"] != float64(tc.status) || diagnostic["upstream_request_id"] != "req_synthetic_upstream") {
				t.Fatal("actual HTTP status or upstream request ID lost", diagnostic)
			}
			if tc.errorStatus != 0 && diagnostic["upstream_error_status"] != float64(tc.errorStatus) {
				t.Fatal("SDK error status lost", diagnostic)
			}
			if tc.code != "" && diagnostic["upstream_error_code"] != tc.code {
				t.Fatal("safe provider error code lost", diagnostic)
			}
			page, err := s.Requests(context.Background(), RequestFilter{RequestID: RequestID(ctx)})
			if err != nil || len(page.Requests) != 1 || page.Requests[0].Outcome != "error" {
				t.Fatal("failed request accounting changed", err)
			}
			if tc.status == 200 && (page.Requests[0].ErrorCode != "upstream_unavailable" || page.Requests[0].UpstreamStatus == nil || *page.Requests[0].UpstreamStatus != 200) {
				t.Fatal("diagnostics changed failed-stream classification", page.Requests[0])
			}
			for _, secret := range []string{"synthetic-private", "sk-synthetic", "user:password", "example.test", "synthetic-token"} {
				if strings.Contains(buffer.String(), secret) {
					t.Fatalf("diagnostic exposed private content: %s", secret)
				}
			}
		})
	}
}

func gatewayLogRecords(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(buffer.Bytes()))
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			return records
		} else if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func TestRetryLogsSafeAttemptMetadata(t *testing.T) {
	var attempts atomic.Int32
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"synthetic-upstream-secret"}`))}, nil
		}
		return syntheticStream(), nil
	}))
	var buffer bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelWarn})))
	x, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-model","input":"synthetic-request-secret"}`), nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Events(func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	x.Body.Close()
	logs := gatewayLogRecords(t, &buffer)
	if len(logs) != 2 || logs[0]["msg"] != "Upstream request failed" || logs[1]["msg"] != "Gateway upstream retry" || logs[1]["upstream_status"] != float64(503) || logs[1]["attempt"] != float64(1) || logs[1]["account_id"] != ids["codex"] {
		t.Fatalf("retry lost safe attempt diagnostics: %+v", logs)
	}
	page, err := s.Requests(context.Background(), RequestFilter{})
	if err != nil || len(page.Requests) != 1 || logs[0]["request_id"] != page.Requests[0].RequestID || logs[1]["request_id"] != page.Requests[0].RequestID {
		t.Fatal("retry correlation was lost", err)
	}
	if strings.Contains(buffer.String(), "synthetic-upstream-secret") || strings.Contains(buffer.String(), "synthetic-request-secret") {
		t.Fatal("retry logged private content")
	}
}

func TestGatewayLogsOutcomesWithSafeCorrelation(t *testing.T) {
	for _, tc := range []struct {
		name, transport, outcome, code, level string
		failure                               error
	}{
		{"HTTP success", "http", "success", "", "DEBUG", nil},
		{"WebSocket success", "websocket", "success", "", "INFO", nil},
		{"failure", "http", "error", "request_failed", "ERROR", errors.New("synthetic-error-secret")},
		{"rate limit", "http", "error", "rate_limited", "WARN", &upstream.UpstreamError{Status: 429, RetryAfter: "synthetic-header-secret"}},
		{"policy rejection", "http", "rejected", "model_not_allowed", "WARN", ErrModelNotAllowed},
		{"disconnect", "http", "canceled", "client_disconnected", "DEBUG", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("observation logging must not contact upstream")
				return nil, nil
			}))
			var buffer bytes.Buffer
			ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
			createAdmissionKey(t, s, 42, 1, 1)
			ctx = WithRequestIdentity(ctx, 42, tc.transport)
			e, err := s.begin(ctx, 1, 1, Responses)
			if err != nil {
				t.Fatal(err)
			}
			e.record.AccountID, e.record.Provider, e.record.Model = ids["codex"], "codex", "synthetic-model"
			if tc.failure != nil {
				e.fail(tc.failure)
			} else {
				e.finish("success", "", "", "")
			}
			e.finish("error", "request_failed", "", "")
			logs := gatewayLogRecords(t, &buffer)
			if len(logs) != 1 {
				t.Fatalf("expected one completion despite repeated settlement, got %d", len(logs))
			}
			log := logs[0]
			if log["msg"] != "Gateway request completed" || log["request_id"] != RequestID(ctx) || log["outcome"] != tc.outcome || log["level"] != tc.level || log["transport"] != tc.transport || log["error_code"] != tc.code {
				t.Fatal("gateway diagnostics lost correlation or outcome", log)
			}
			if log["tenant_id"] != float64(1) || log["user_id"] != float64(1) || log["key_id"] != float64(42) || log["group_id"] != float64(1) || log["account_id"] != ids["codex"] || log["model"] != "synthetic-model" {
				t.Fatal("gateway diagnostics omitted the safe actor context", log)
			}
			if tc.name == "rate limit" && log["upstream_status"] != float64(429) {
				t.Fatal("upstream status was omitted", log)
			}
			if strings.Contains(buffer.String(), "synthetic-error-secret") || strings.Contains(buffer.String(), "synthetic-header-secret") {
				t.Fatal("gateway diagnostics exposed raw errors or headers")
			}
		})
	}
}

type gatewayLogProbe struct {
	bytes.Buffer
	service *Service
	test    *testing.T
}

func (p *gatewayLogProbe) Write(data []byte) (int, error) {
	if !p.service.mu.TryLock() {
		p.test.Error("log output held the gateway admission lock")
	} else {
		p.service.mu.Unlock()
	}
	return p.Buffer.Write(data)
}

func TestGatewayLogsPersistenceFailureOutsideAdmissionLock(t *testing.T) {
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("observation logging must not contact upstream")
		return nil, nil
	}))
	probe := &gatewayLogProbe{service: s, test: t}
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(probe, nil)))
	createAdmissionKey(t, s, 42, 1, 1)
	ctx = WithRequestIdentity(ctx, 42, "http")
	e, err := s.begin(ctx, 1, 1, Responses)
	if err != nil {
		t.Fatal(err)
	}
	e.record.AccountID, e.record.Provider, e.record.Model = ids["codex"], "codex", "synthetic-model"
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	e.finish("success", "", "", "")
	logs := gatewayLogRecords(t, &probe.Buffer)
	if len(logs) != 1 || logs[0]["level"] != "ERROR" || logs[0]["error_code"] != "persist_request_failed" || logs[0]["request_id"] != RequestID(ctx) || logs[0]["account_id"] != ids["codex"] {
		t.Fatal("persistence failure lacks request context", logs)
	}
}
