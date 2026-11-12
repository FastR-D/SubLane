package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"testing/iotest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/gateway"
	"github.com/murongg/SubLane/internal/logging"
	"github.com/murongg/SubLane/internal/testdb"
)

type logBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *logBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b.Buffer.Bytes()))
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

func captureLogs(t *testing.T, level slog.Level) *logBuffer {
	t.Helper()
	buffer := &logBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}

func TestServerLogsFailuresWithoutSensitiveInputs(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	handler := New(Options{})
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login?token=synthetic-query-secret", strings.NewReader(`{"password":"synthetic-body-secret"}`))
	r.Header.Set("Authorization", "Bearer synthetic-header-secret")
	r.Header.Set("Cookie", "session=synthetic-cookie-secret")
	r.Header.Set("X-Request-ID", "synthetic-caller-id")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("unexpected response", w.Code)
	}
	logs := buffer.records(t)
	if len(logs) != 1 {
		t.Fatalf("expected one HTTP summary, got %d", len(logs))
	}
	log := logs[0]
	id := w.Header().Get("X-Request-ID")
	if id == "" || id == "synthetic-caller-id" || log["request_id"] != id {
		t.Fatal("request ID was not generated and correlated", log)
	}
	if log["msg"] != "HTTP request completed" || log["level"] != "ERROR" || log["method"] != "POST" || log["status"] != float64(w.Code) || log["route"] != "/api/auth/login" {
		t.Fatal("missing request metadata", log)
	}
	if duration, ok := log["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatal("invalid request duration", log)
	}
	if log["bytes"] != float64(w.Body.Len()) {
		t.Fatal("response byte count differs", log)
	}
	raw, _ := json.Marshal(log)
	for _, secret := range []string{"synthetic-query-secret", "synthetic-body-secret", "synthetic-header-secret", "synthetic-cookie-secret", "synthetic-caller-id"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("request log leaked sensitive input", secret)
		}
	}
}

func TestServerLogsNestedRoutersOnce(t *testing.T) {
	buffer := captureLogs(t, slog.LevelDebug)
	handler := NewMulti(nil, nil, nil, "", func(int64) http.Handler { return New(Options{}) })
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	logs := buffer.records(t)
	if w.Code != http.StatusOK || len(logs) != 1 || logs[0]["route"] != "/healthz" || logs[0]["level"] != "DEBUG" || logs[0]["request_id"] != w.Header().Get("X-Request-ID") {
		t.Fatal("nested routing duplicated or lost the summary", w.Code, logs)
	}
}

func TestServerLogsQuietHealthAndStaticSuccess(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		t.Run(level.String(), func(t *testing.T) {
			buffer := captureLogs(t, level)
			handler := New(Options{Ping: func(context.Context) error { return nil }, Assets: fstest.MapFS{"index.html": {Data: []byte("synthetic page")}}})
			for _, path := range []string{"/healthz", "/readyz", "/"} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusOK || w.Header().Get("X-Request-ID") == "" {
					t.Fatal("successful request lost its diagnostic ID", path, w.Code)
				}
			}
			logs := buffer.records(t)
			if level == slog.LevelInfo && len(logs) != 0 || level == slog.LevelDebug && len(logs) != 3 {
				t.Fatal("quiet requests did not respect the configured level", logs)
			}
		})
	}
}

func TestServerLogsUnmatchedPathsWithoutRawURLs(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	handler := New(Options{})
	for _, check := range []struct{ method, path string }{
		{http.MethodGet, "/v0/synthetic-path-secret?key=synthetic-query-secret"},
		{http.MethodPost, "/synthetic-path-secret"},
		{"SYNTHETIC_SECRET_METHOD", "/synthetic-path-secret"},
	} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(check.method, check.path, nil))
	}
	logs := buffer.records(t)
	if len(logs) != 3 {
		t.Fatal("unmatched requests were not recorded", logs)
	}
	for _, log := range logs {
		raw, _ := json.Marshal(log)
		if log["level"] != "WARN" || strings.Contains(string(raw), "synthetic-path-secret") || strings.Contains(string(raw), "synthetic-query-secret") || strings.Contains(string(raw), "SYNTHETIC_SECRET_METHOD") {
			t.Fatal("unmatched route exposed raw input or lost its level", log)
		}
	}
}

func TestServerLogsPanicsWithoutPanicContents(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	handler := New(Options{Ping: func(context.Context) error { panic("synthetic-panic-secret") }})
	w := httptest.NewRecorder()
	func() {
		defer func() {
			if failure := recover(); failure != http.ErrAbortHandler {
				t.Fatal("panic did not retain HTTP abort semantics", failure)
			}
		}()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	}()
	logs := buffer.records(t)
	if len(logs) != 1 || logs[0]["level"] != "ERROR" || logs[0]["error_code"] != "handler_panic" || logs[0]["request_id"] != w.Header().Get("X-Request-ID") {
		t.Fatal("panic was not safely reported", w.Code, logs)
	}
	raw, _ := json.Marshal(logs)
	if strings.Contains(string(raw), "synthetic-panic-secret") || !strings.Contains(string(raw), "logging_test.go") {
		t.Fatal("panic diagnostics leaked contents or omitted the stack location", string(raw))
	}
}

func TestHTTPLogIDMatchesGatewayHistory(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	f := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"synthetic\",\"output\":[]}}\n\n")
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"synthetic-model","input":"synthetic-prompt-secret"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+f.secret)
	w := httptest.NewRecorder()
	f.server.Config.Handler.ServeHTTP(w, r)
	id := w.Header().Get("X-Request-ID")
	page, err := f.forwarding.UserRequests(context.Background(), f.userID, gateway.RequestFilter{RequestID: id})
	if err != nil || w.Code != http.StatusOK || len(page.Requests) != 1 {
		t.Fatal("gateway request failed", w.Code, err)
	}
	logs := buffer.records(t)
	if len(logs) != 1 || logs[0]["request_id"] != page.Requests[0].RequestID || logs[0]["route"] != "/v1/responses" || logs[0]["level"] != "INFO" {
		t.Fatal("HTTP log and request history do not correlate", logs)
	}
}

func TestServerLogsRouteTemplatesAndCommittedPanics(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	router := chi.NewRouter()
	router.Use(requestLogging)
	router.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "synthetic response")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		panic([]byte("synthetic-panic-secret"))
	})
	w := httptest.NewRecorder()
	func() {
		defer func() {
			if failure := recover(); failure != http.ErrAbortHandler {
				t.Fatal("committed response did not abort", failure)
			}
		}()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/items/synthetic-path-secret", nil))
	}()
	logs := buffer.records(t)
	if w.Body.String() != "synthetic response" || len(logs) != 1 || logs[0]["route"] != "/items/{id}" || logs[0]["status"] != float64(http.StatusOK) || logs[0]["error_code"] != "handler_panic" {
		t.Fatal("committed panic corrupted the response or route metadata", w.Body.String(), logs)
	}
	raw, _ := json.Marshal(logs)
	if strings.Contains(string(raw), "synthetic-path-secret") || strings.Contains(string(raw), "synthetic-panic-secret") {
		t.Fatal("panic log leaked caller input", string(raw))
	}
}

func TestWebsocketLogsUpgradeAndCorrelation(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	f := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"synthetic\",\"output\":[]}}\n\n")
	})
	conn, response, err := websocket.DefaultDialer.Dial(strings.Replace(f.server.URL, "http://", "ws://", 1)+"/v1/responses", http.Header{"Authorization": {"Bearer " + f.secret}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	id := response.Header.Get("X-Request-ID")
	conn.Close()
	if id == "" {
		t.Fatal("websocket handshake lost its diagnostic ID")
	}
	deadline := time.Now().Add(time.Second)
	for {
		logs := buffer.records(t)
		if len(logs) > 0 {
			if len(logs) != 1 || logs[0]["request_id"] != id || logs[0]["status"] != float64(http.StatusSwitchingProtocols) {
				t.Fatal("websocket summary lost upgrade metadata", logs)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("websocket summary was not emitted on connection close")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWebsocketLogsRejectedMessagesWithoutSensitiveInputs(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason, code string
	}{
		{"invalid JSON", `{"input":"synthetic-body-secret"`, "invalid_json", "invalid_model_request"},
		{"invalid type", `{"type":"synthetic-type-secret","model":"synthetic-model-secret"}`, "invalid_type", "invalid_model_request"},
		{"missing model", `{"type":"response.create","input":"synthetic-body-secret"}`, "invalid_model", "invalid_model_request"},
		{"invalid previous ID", `{"type":"response.create","model":"synthetic-model-secret","previous_response_id":{"private":"synthetic-body-secret"}}`, "invalid_previous_response_id", "invalid_model_request"},
		{"invalid input", `{"type":"response.create","model":"synthetic-model-secret","input":{"private":"synthetic-body-secret"}}`, "invalid_input", "invalid_model_request"},
		{"unknown response", `{"type":"response.create","model":"synthetic-model-secret","previous_response_id":"synthetic-response-secret","input":[]}`, "unknown_response", "continuation_requires_full_input"},
		{"missing history", `{"type":"response.append","model":"synthetic-model-secret","input":[]}`, "missing_history", "continuation_requires_full_input"},
		{"invalid generate", `{"type":"response.create","model":"synthetic-model-secret","generate":"synthetic-body-secret"}`, "invalid_generate", "invalid_model_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := captureLogs(t, slog.LevelInfo)
			f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) {
				t.Error("rejected message reached upstream")
			})
			conn, response, err := websocket.DefaultDialer.Dial(strings.Replace(f.server.URL, "http://", "ws://", 1)+"/v1/responses?token=synthetic-query-secret", http.Header{
				"Authorization": {"Bearer " + f.secret},
				"Cookie":        {"session=synthetic-cookie-secret"},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(tc.body)); err != nil {
				t.Fatal(err)
			}
			var event struct {
				Type      string `json:"type"`
				Status    int    `json:"status"`
				RequestID string `json:"request_id"`
				Error     struct {
					Code, Message string
				} `json:"error"`
			}
			if err := conn.ReadJSON(&event); err != nil {
				t.Fatal(err)
			}
			if event.Type != "error" || event.Status != 400 || event.Error.Code != tc.code || event.Error.Message != strings.ReplaceAll(tc.code, "_", " ") || event.RequestID == "" {
				t.Fatal("diagnostics changed the client error", event)
			}
			records := buffer.records(t)
			var logs []map[string]any
			for _, record := range records {
				if record["msg"] == "Gateway request rejected" {
					logs = append(logs, record)
				}
			}
			if len(logs) != 1 || logs[0]["msg"] != "Gateway request rejected" || logs[0]["level"] != "WARN" || logs[0]["transport"] != "websocket" || logs[0]["stage"] != "normalize" || logs[0]["reason"] != tc.reason || logs[0]["error_code"] != tc.code || logs[0]["status"] != float64(400) {
				t.Fatal("missing rejection diagnostics", logs)
			}
			if logs[0]["connection_id"] != response.Header.Get("X-Request-ID") || logs[0]["request_id"] != event.RequestID || logs[0]["user_id"] != float64(f.userID) || logs[0]["key_id"] != float64(f.keyID) || logs[0]["group_id"] != float64(1) || logs[0]["tenant_id"] != float64(1) {
				t.Fatal("rejection lost trusted identity or connection correlation", logs)
			}
			page, err := f.forwarding.Requests(context.Background(), gateway.RequestFilter{})
			if err != nil || page.Total != 1 || page.Requests[0].RequestID != event.RequestID || page.Requests[0].ErrorCode != "invalid_request" || page.Requests[0].Outcome != "rejected" {
				t.Fatal("validation failure missing from request history", page.Total, err)
			}
			raw, _ := json.Marshal(records)
			for _, secret := range []string{f.secret, "synthetic-body-secret", "synthetic-model-secret", "synthetic-response-secret", "synthetic-type-secret", "synthetic-query-secret", "synthetic-cookie-secret"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("rejection log leaked caller input", secret)
				}
			}
			if err := conn.WriteJSON(map[string]any{"type": "response.create", "model": "synthetic-model", "generate": false, "input": []any{}}); err != nil {
				t.Fatal(err)
			}
			for {
				if err := conn.ReadJSON(&event); err != nil {
					t.Fatal(err)
				}
				if event.Type == "error" {
					t.Fatal("rejected message prevented a valid retry", event)
				}
				if event.Type == "response.completed" {
					break
				}
			}
			if len(buffer.records(t)) != len(records) {
				t.Fatal("successful retry emitted a rejection diagnostic")
			}
		})
	}
}

func TestHTTPLogsRejectedRequestParsingWithoutSensitiveInputs(t *testing.T) {
	for _, tc := range []struct{ name, path, body, stage string }{
		{"invalid JSON", "/v1/responses", `{"input":"synthetic-body-secret"`, "request_flags"},
		{"invalid stream", "/v1/responses", `{"model":"synthetic-model-secret","stream":"synthetic-body-secret"}`, "request_flags"},
		{"streaming compact", "/v1/responses/compact", `{"model":"synthetic-model-secret","stream":true}`, "compact_stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := captureLogs(t, slog.LevelInfo)
			f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) {
				t.Error("rejected request reached upstream")
			})
			r := httptest.NewRequest(http.MethodPost, tc.path+"?token=synthetic-query-secret", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.secret)
			w := httptest.NewRecorder()
			f.server.Config.Handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"invalid_model_request"`) {
				t.Fatal("diagnostics changed request validation", w.Code, w.Body.String())
			}
			logs := buffer.records(t)
			if len(logs) < 2 || logs[0]["msg"] != "Gateway request rejected" || logs[0]["level"] != "WARN" || logs[0]["transport"] != "http" || logs[0]["stage"] != tc.stage || logs[0]["status"] != float64(400) || logs[0]["error_code"] != "invalid_model_request" || logs[0]["request_id"] != w.Header().Get("X-Request-ID") || logs[len(logs)-1]["request_id"] != logs[0]["request_id"] {
				t.Fatal("missing correlated HTTP rejection diagnostics", logs)
			}
			page, err := f.forwarding.Requests(context.Background(), gateway.RequestFilter{})
			if err != nil || page.Total != 1 || page.Requests[0].RequestID != w.Header().Get("X-Request-ID") || page.Requests[0].Outcome != "rejected" || page.Requests[0].ErrorCode != "invalid_request" {
				t.Fatal("parsing failure missing from request history", page.Total, err)
			}
			raw, _ := json.Marshal(logs)
			for _, secret := range []string{f.secret, "synthetic-body-secret", "synthetic-model-secret", "synthetic-query-secret"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("rejection log leaked caller input", secret)
				}
			}
		})
	}
}

func TestHTTPLogsBodyReadFailuresWithoutSensitiveInputs(t *testing.T) {
	for _, tc := range []struct {
		name, reason, code string
		status             int
		err                error
	}{
		{"timeout", "timeout", "request_body_timeout", 408, context.DeadlineExceeded},
		{"incomplete upload", "unexpected_eof", "request_body_read_failed", 400, io.ErrUnexpectedEOF},
		{"other read failure", "read_error", "request_body_read_failed", 400, errors.New("synthetic-read-secret")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := captureLogs(t, slog.LevelInfo)
			f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) {
				t.Error("incomplete request reached upstream")
			})
			const partial = `{"input":"synthetic-body-secret"`
			r := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			r.Body = io.NopCloser(cancelingReader{reader: io.MultiReader(strings.NewReader(partial), iotest.ErrReader(tc.err)), cancel: cancel})
			r.ContentLength = int64(len(partial) + 4096)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.secret)
			w := httptest.NewRecorder()
			f.server.Config.Handler.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal("read failure response changed", w.Code, w.Body.String())
			}
			logs := buffer.records(t)
			if len(logs) < 2 || logs[0]["msg"] != "Gateway request rejected" || logs[0]["stage"] != "read_body" || logs[0]["reason"] != tc.reason || logs[0]["received_bytes"] != float64(len(partial)) || logs[0]["content_length"] != float64(r.ContentLength) || logs[0]["request_id"] != w.Header().Get("X-Request-ID") {
				t.Fatal("missing read failure diagnostics", logs)
			}
			page, err := f.forwarding.Requests(context.Background(), gateway.RequestFilter{})
			if err != nil || page.Total != 1 {
				t.Fatal("incomplete upload missing from request history", page.Total, err)
			}
			record := page.Requests[0]
			if record.ErrorCode != tc.code || record.Outcome != "rejected" || record.RequestID != w.Header().Get("X-Request-ID") || record.KeyID != f.keyID || record.AccountID != "" || record.UpstreamStatus != nil || record.InputTokens != nil || record.OutputTokens != nil || record.EstimatedCostMicroUSD != nil {
				t.Fatal("ingress failure charged usage or lost correlation", record)
			}
			raw, _ := json.Marshal(logs)
			for _, secret := range []string{f.secret, "synthetic-body-secret", "synthetic-read-secret"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("read failure log leaked caller input", secret)
				}
			}
		})
	}
}

func TestWebsocketLogsConnectionLimitRejections(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("handshake reached upstream")
	})
	url := strings.Replace(f.server.URL, "http://", "ws://", 1) + "/v1/responses"
	headers := http.Header{"Authorization": {"Bearer " + f.secret}}
	for range 8 {
		conn, _, err := websocket.DefaultDialer.Dial(url, headers)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
	}
	conn, response, err := websocket.DefaultDialer.Dial(url, headers)
	if conn != nil {
		conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatal("connection limit behavior changed", response, err)
	}
	defer response.Body.Close()
	found := false
	for _, record := range buffer.records(t) {
		if record["msg"] == "Gateway request rejected" {
			found = true
			if record["stage"] != "connection_admission" || record["error_code"] != "gateway_busy" || record["connections"] != float64(8) || record["connection_limit"] != float64(8) || record["connection_id"] != response.Header.Get("X-Request-ID") {
				t.Fatal("missing connection limit diagnostics", record)
			}
		}
	}
	if !found {
		t.Fatal("connection limit rejection was not logged")
	}
}

func TestAuditFailureLogKeepsSafeRequestContext(t *testing.T) {
	connection, err := testdb.Open(context.Background(), filepath.Join(t.TempDir(), "synthetic-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	service := audit.New(connection)
	connection.Close()
	buffer := &logBuffer{}
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(buffer, nil)))
	ctx = gateway.WithRequestIdentity(ctx, 0, "http")
	h := &authHTTP{audit: service}
	r := httptest.NewRequest(http.MethodPost, "/api/groups?token=synthetic-query-secret", strings.NewReader(`{"name":"synthetic-body-secret"}`)).WithContext(ctx)
	h.auditRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }), httptest.NewRecorder(), r)
	logs := buffer.records(t)
	if len(logs) != 1 || logs[0]["request_id"] != gateway.RequestID(ctx) || logs[0]["action"] != "group.create" || logs[0]["resource"] != "group" || logs[0]["status"] != float64(503) {
		t.Fatal("audit persistence error has no request context", logs)
	}
	raw, _ := json.Marshal(logs)
	if strings.Contains(string(raw), "synthetic-query-secret") || strings.Contains(string(raw), "synthetic-body-secret") {
		t.Fatal("audit diagnostics exposed the request", string(raw))
	}
}

func TestLoggingDoesNotBufferStreamingResponses(t *testing.T) {
	buffer := captureLogs(t, slog.LevelInfo)
	released := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(released) }) })
	router := chi.NewRouter()
	router.Use(requestLogging)
	router.Get("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: synthetic-first-event\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-released:
			io.WriteString(w, "data: synthetic-last-event\n\n")
		case <-r.Context().Done():
		}
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(server.URL + "/stream")
	if err != nil {
		release.Do(func() { close(released) })
		t.Fatal("stream did not flush before handler completion", err)
	}
	defer response.Body.Close()
	first := make([]byte, len("data: synthetic-first-event\n\n"))
	if _, err := io.ReadFull(response.Body, first); err != nil || string(first) != "data: synthetic-first-event\n\n" {
		t.Fatal("first event was buffered or changed", string(first), err)
	}
	if len(buffer.records(t)) != 0 {
		t.Fatal("stream summary emitted before the request finished")
	}
	release.Do(func() { close(released) })
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	logs := buffer.records(t)
	if len(logs) != 1 || logs[0]["status"] != float64(200) || logs[0]["bytes"] != float64(len("data: synthetic-first-event\n\ndata: synthetic-last-event\n\n")) {
		t.Fatal("stream summary did not describe the complete response", logs)
	}
}

type failedFlushWriter struct {
	*httptest.ResponseRecorder
	failure error
}

func (w *failedFlushWriter) FlushError() error { return w.failure }

func TestHTTPLoggingPreservesFlushErrors(t *testing.T) {
	captureLogs(t, slog.LevelInfo)
	w := &failedFlushWriter{ResponseRecorder: httptest.NewRecorder(), failure: errors.New("synthetic-flush-error")}
	router := chi.NewRouter()
	router.Use(requestLogging)
	router.Get("/stream", func(writer http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(writer).Flush(); !errors.Is(err, w.failure) {
			t.Error("response wrapper discarded the flush failure", err)
		}
	})
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stream", nil))
}
