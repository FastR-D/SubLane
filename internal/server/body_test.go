package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/murongg/SubLane/internal/gateway"
)

type uploadDeadlines struct{ http.ResponseWriter }

type cancelingReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r cancelingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil {
		r.cancel()
	}
	return n, err
}

func (w uploadDeadlines) SetReadDeadline(value time.Time) error {
	if !value.IsZero() {
		value = time.Now().Add(time.Second)
	}
	return http.NewResponseController(w.ResponseWriter).SetReadDeadline(value)
}

func TestGatewayAcceptsUploadWithContinuedProgress(t *testing.T) {
	f := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_synthetic\",\"output\":[]}}\n\n")
	})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.server.Config.Handler.ServeHTTP(uploadDeadlines{w}, r)
	}))
	server.Config.ReadTimeout = time.Second
	server.Start()
	defer server.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	const body = `{"model":"synthetic-model","input":"synthetic upload"}`
	if _, err := fmt.Fprintf(conn, "POST /v1/responses HTTP/1.1\r\nHost: example.test\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", f.secret, len(body)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	stop, done := make(chan struct{}), make(chan struct{})
	var writeErr error
	go func() {
		defer close(done)
		for i := range 4 {
			if i > 0 {
				select {
				case <-time.After(400 * time.Millisecond):
				case <-stop:
					return
				}
			}
			if _, writeErr = io.WriteString(conn, body[len(body)*i/4:len(body)*(i+1)/4]); writeErr != nil {
				return
			}
		}
	}()
	defer func() { close(stop); conn.Close(); <-done }()
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatal("active upload exceeded a fixed total read deadline", response.StatusCode)
	}
	<-done
	if writeErr != nil {
		t.Fatal(writeErr)
	}
}

func TestGatewayDoesNotRecordUnauthenticatedUploads(t *testing.T) {
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("unauthenticated request reached upstream")
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"synthetic"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.server.Config.Handler.ServeHTTP(w, r)
	page, err := f.forwarding.Requests(context.Background(), gateway.RequestFilter{})
	if w.Code != http.StatusUnauthorized || err != nil || page.Total != 0 {
		t.Fatal("unauthenticated upload entered member history", w.Code, page.Total, err)
	}
}

func TestGatewayStalledUploadReturnsTimeoutAndHistory(t *testing.T) {
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) {
		t.Error("stalled upload reached upstream")
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.server.Config.Handler.ServeHTTP(uploadDeadlines{w}, r)
	}))
	defer server.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "POST /v1/responses HTTP/1.1\r\nHost: example.test\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{", f.secret); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestTimeout || !response.Close {
		t.Fatal("stalled upload did not close with HTTP 408", response.StatusCode, response.Close)
	}
	page, err := f.forwarding.UserRequests(context.Background(), f.userID, gateway.RequestFilter{RequestID: response.Header.Get("X-Request-ID")})
	if err != nil || page.Total != 1 || page.Requests[0].ErrorCode != "request_body_timeout" || page.Requests[0].DurationMs < 900 {
		t.Fatal("transport cancellation lost the upload timeout record", page, err)
	}
}

func TestUploadReaderKeepsItsTotalBudget(t *testing.T) {
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	body := strings.NewReader("synthetic upload")
	deadline := time.Now().Add(time.Minute)
	r := &uploadReader{reader: body, controller: http.NewResponseController(w), idle: time.Hour, deadline: deadline}
	r.renewDeadline()
	if _, err := r.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	for _, got := range w.deadlines {
		if !got.Equal(deadline) {
			t.Fatal("progress extended the total upload budget", got, deadline)
		}
	}
	r.deadline = time.Now().Add(-time.Second)
	remaining := body.Len()
	if n, err := r.Read(make([]byte, 1)); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) || body.Len() != remaining {
		t.Fatal("expired upload budget still read data", n, err)
	}
}

func TestUploadReadDeadlineIsClearedAfterFailure(t *testing.T) {
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	_, err := readRequestBody(http.NewResponseController(w), iotest.ErrReader(context.DeadlineExceeded), gateway.DefaultPolicy())
	if !errors.Is(err, context.DeadlineExceeded) || len(w.deadlines) < 2 || w.deadlines[0].IsZero() || !w.deadlines[len(w.deadlines)-1].IsZero() {
		t.Fatal("upload deadline leaked into response processing", err, w.deadlines)
	}
}

func TestUploadReadUsesConfiguredBudgets(t *testing.T) {
	for _, total := range []time.Duration{20 * time.Second, 3 * time.Minute} {
		t.Run(total.String(), func(t *testing.T) {
			policy := gateway.DefaultPolicy()
			policy.UploadIdleTimeout, policy.UploadTimeout = time.Minute, total
			w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			before := time.Now()
			body, err := readRequestBody(http.NewResponseController(w), strings.NewReader("synthetic upload"), policy)
			if err != nil || string(body) != "synthetic upload" || len(w.deadlines) < 2 {
				t.Fatal("configured upload failed", err, w.deadlines)
			}
			budget := min(policy.UploadIdleTimeout, total)
			if got := w.deadlines[0]; got.Before(before.Add(budget)) || got.After(time.Now().Add(budget)) || !w.deadlines[len(w.deadlines)-1].IsZero() {
				t.Fatal("upload did not honor or clear its configured read deadline", w.deadlines)
			}
		})
	}
}

func TestNativeUploadTimeoutErrors(t *testing.T) {
	w := httptest.NewRecorder()
	if got := messagesErrorBody(w, 408, "request_body_timeout")["error"].(map[string]string)["type"]; got != "timeout_error" {
		t.Fatal("native Messages upload timeout was misclassified", got)
	}
	if got := geminiErrorBody(408, "request_body_timeout")["error"].(map[string]any)["status"]; got != "DEADLINE_EXCEEDED" {
		t.Fatal("native Gemini upload timeout was misclassified", got)
	}
}
