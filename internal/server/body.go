package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/murongg/SubLane/internal/gateway"
)

var (
	errRequestBodyTimeout = errors.New("request_body_timeout")
	errRequestBodyRead    = errors.New("request_body_read_failed")
)

type uploadReader struct {
	reader     io.Reader
	controller *http.ResponseController
	idle       time.Duration
	deadline   time.Time
}

func (r *uploadReader) renewDeadline() {
	deadline := time.Now().Add(r.idle)
	if r.deadline.Before(deadline) {
		deadline = r.deadline
	}
	_ = r.controller.SetReadDeadline(deadline)
}

func (r *uploadReader) Read(p []byte) (int, error) {
	// A trickle of bytes must not retain a gateway slot indefinitely.
	if !time.Now().Before(r.deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	n, err := r.reader.Read(p)
	// Zero-byte reads do not prove upload progress.
	if n > 0 && err == nil {
		r.renewDeadline()
	}
	return n, err
}

func readRequestBody(controller *http.ResponseController, body io.Reader, policy gateway.Policy) ([]byte, error) {
	r := &uploadReader{reader: body, controller: controller, idle: policy.UploadIdleTimeout, deadline: time.Now().Add(policy.UploadTimeout)}
	r.renewDeadline()
	// An upload deadline must never become a deadline on generation or the next keep-alive request.
	defer controller.SetReadDeadline(time.Time{})
	return io.ReadAll(r)
}
