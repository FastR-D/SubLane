package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/storage/db"
)

func TestRequestIdentityAttachesAuthenticationButDoesNotReuseCompletedIdentity(t *testing.T) {
	pending := WithRequestIdentity(context.Background(), 0, "http")
	admitted := WithRequestIdentity(pending, 42, "http")
	if RequestID(pending) != RequestID(admitted) {
		t.Fatal("native header and record IDs diverged")
	}
	next := WithRequestIdentity(admitted, 42, "http")
	if RequestID(admitted) == RequestID(next) {
		t.Fatal("new request reused an admitted request ID")
	}
}

func TestDiagnosticsMeasuresFirstOutputNotCreatedOrCompletion(t *testing.T) {
	for _, delta := range []string{"response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta", ""} {
		t.Run(delta, func(t *testing.T) {
			stream := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"synthetic\"}}\n\n"
			if delta != "" {
				stream += "data: {\"type\":\"" + delta + "\",\"delta\":\"synthetic\"}\n\n"
			}
			stream += "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"synthetic\",\"output\":[]}}\n\n"
			s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, nil
			}))
			clock := time.Now()
			s.now = func() time.Time { return clock }
			createAdmissionKey(t, s, 42, 1, 1)
			ctx := WithRequestIdentity(context.Background(), 42, "http")
			x, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-model","input":"synthetic"}`), nil, Responses)
			if err != nil {
				t.Fatal(err)
			}
			if err := x.Events(func([]byte) error { clock = clock.Add(30 * time.Millisecond); return nil }); err != nil {
				t.Fatal(err)
			}
			x.Body.Close()
			page, err := s.Requests(ctx, RequestFilter{})
			if err != nil || len(page.Requests) != 1 {
				t.Fatal(err)
			}
			r := page.Requests[0]
			if r.RequestID == "" || r.RequestID != RequestID(ctx) {
				t.Fatal("correlation lost", r.RequestID)
			}
			if delta == "" {
				if r.FirstTokenMs != nil {
					t.Fatal("completion fabricated first token")
				}
			} else if r.FirstTokenMs == nil || *r.FirstTokenMs != 30 {
				t.Fatal("wrong first output latency", r.FirstTokenMs)
			}
		})
	}
}

func TestDiagnosticFiltersComposeBeforePaginationAndPreserveOwner(t *testing.T) {
	ctx := context.Background()
	s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	now := time.Now().Unix()
	for i := int64(1); i <= 120; i++ {
		model := "synthetic-a"
		if i%2 == 0 {
			model = "synthetic-b"
		}
		if err := s.queries.RecordRequest(ctx, db.RecordRequestParams{UserID: i%3 + 1, KeyID: i%4 + 1, GroupID: 1, Model: model, Transport: "http", Operation: "responses", Outcome: "success", StartedAt: now - i, RequestID: "synthetic-id"}); err != nil {
			t.Fatal(err)
		}
	}
	filter := RequestFilter{Model: "synthetic-a", KeyID: 2, From: now - 90, Until: now - 10, MemberID: 2, RequestID: "synthetic-id"}
	page, err := s.Requests(ctx, filter)
	if err != nil || len(page.Requests) != 7 || page.Total != 7 {
		t.Fatal("composed filters incorrect", len(page.Requests), err)
	}
	for _, r := range page.Requests {
		if r.UserID != 2 || r.KeyID != 2 || r.Model != "synthetic-a" || r.StartedAt < filter.From || r.StartedAt >= filter.Until {
			t.Fatal("filter escaped", r)
		}
	}
	filter.MemberID = 1
	filter.AccountID = "another-account"
	personal, err := s.UserRequests(ctx, 2, filter)
	if err != nil || len(personal.Requests) != len(page.Requests) || personal.Total != page.Total {
		t.Fatal("personal filters changed SQL ownership", err, len(personal.Requests))
	}
	for _, bad := range []RequestFilter{{From: now, Until: now - 1}, {KeyID: -1}, {MemberID: -1}, {Model: strings.Repeat("x", 161)}, {RequestID: "unsafe\nidentifier"}} {
		if _, err := s.Requests(ctx, bad); err == nil {
			t.Fatal("invalid filter admitted", bad)
		}
	}
}

func TestRequestsRecordReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		name, fields, want, transport string
		kind                          Kind
		rejected                      bool
	}{
		{name: "responses", fields: `,"reasoning":{"effort":"high"}`, want: "high", kind: Responses},
		{name: "websocket", fields: `,"reasoning":{"effort":"xhigh"}`, want: "xhigh", kind: Responses, transport: "websocket"},
		{name: "chat", fields: `,"reasoning_effort":"low"`, want: "low", kind: Chat},
		{name: "rejected", fields: `,"reasoning":{"effort":"medium"}`, want: "medium", kind: Responses, rejected: true},
		{name: "compact", fields: `,"reasoning":{"effort":"minimal"}`, want: "minimal", kind: Compact, rejected: true},
		{name: "none", fields: `,"reasoning":{"effort":"none"}`, want: "none", kind: Responses},
		{name: "max", fields: `,"reasoning":{"effort":"max"}`, want: "max", kind: Responses},
		{name: "ultra", fields: `,"reasoning":{"effort":"ultra"}`, want: "ultra", kind: Responses},
		{name: "omitted", kind: Responses},
		{name: "unknown", fields: `,"reasoning":{"effort":"synthetic-private-text"}`, kind: Responses},
		{name: "wrong type", fields: `,"reasoning":{"effort":42}`, kind: Responses},
		{name: "malformed reasoning", fields: `,"reasoning":[]`, kind: Responses},
		{name: "chat field on responses", fields: `,"reasoning_effort":"high"`, kind: Responses},
		{name: "responses field on chat", fields: `,"reasoning":{"effort":"high"}`, kind: Chat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
			createAdmissionKey(t, s, 42, 1, 1)
			ctx := WithRequestIdentity(context.Background(), 42, tc.transport)
			model := "synthetic-model"
			if tc.rejected {
				model = "synthetic-unsupported-model"
			}
			raw := []byte(`{"model":"` + model + `","input":"synthetic","messages":[{"role":"user","content":"synthetic"}]` + tc.fields + `}`)
			x, err := s.Open(ctx, 1, 1, raw, nil, tc.kind)
			if tc.rejected {
				if !errors.Is(err, ErrModelUnavailable) {
					t.Fatal("expected model rejection", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := x.Events(func([]byte) error { return nil }); err != nil {
					t.Fatal(err)
				}
				x.Body.Close()
			}
			for _, owner := range []int64{0, 1} {
				page, err := s.requests(ctx, owner, RequestFilter{})
				if err != nil || len(page.Requests) != 1 {
					t.Fatal("request history", err, len(page.Requests))
				}
				encoded, err := json.Marshal(page.Requests[0])
				if err != nil {
					t.Fatal(err)
				}
				var metadata map[string]any
				if err := json.Unmarshal(encoded, &metadata); err != nil {
					t.Fatal(err)
				}
				if metadata["reasoning_effort"] != tc.want {
					t.Fatalf("owner=%d reasoning_effort=%v want=%q", owner, metadata["reasoning_effort"], tc.want)
				}
			}
		})
	}
}
