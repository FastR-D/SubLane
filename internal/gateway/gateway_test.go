package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/upstream"
	"github.com/murongg/SubLane/internal/vault"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGatewayKeepsAccountAffinityAndBoundsActiveRequests(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	connection, err := storage.Open(ctx, filepath.Join(dir, "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Setup(ctx, "owner-test", "owner pass 42", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(connection, cipher)
	mapping := map[string]string{}
	for _, id := range []string{"upstream-one", "upstream-two"} {
		row, err := service.Authorize(ctx, id, accounts.Credential{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", AccountID: id, ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
		if err != nil {
			t.Fatal(err)
		}
		mapping[id] = row.ID
		if err := service.SaveCatalog(ctx, row.ID, 0, []string{"synthetic-model"}, time.Now().Unix(), upstream.CatalogSource("codex")); err != nil {
			t.Fatal(err)
		}
	}
	configureTestPool(t, connection)
	var mu sync.Mutex
	var used []string
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/wham/usage" {
			io.WriteString(w, `{"rate_limit":{}}`)
			return
		}
		if r.URL.Path == "/backend-api/wham/rate-limit-reset-credits" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		used = append(used, r.Header.Get("Chatgpt-Account-Id"))
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-access" {
			t.Error("wrong upstream credential")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"output\":[]}}\n\n")
	}))
	defer fakeUpstream.Close()
	target, _ := url.Parse(fakeUpstream.URL)
	client := upstream.NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		copy.URL.Scheme = target.Scheme
		copy.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(copy)
	}))
	defer client.Close()
	gateway := New(ctx, connection, service, client)
	defer gateway.Close()
	headers := http.Header{"Session_id": {"synthetic-conversation"}}
	raw := []byte(`{"model":"synthetic-model","input":"synthetic prompt"}`)
	for range 2 {
		stream, err := gateway.Open(ctx, 1, 1, raw, headers, Responses)
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Events(func([]byte) error { return nil }); err != nil {
			t.Fatal(err)
		}
		stream.Body.Close()
	}
	mu.Lock()
	selected := used[0]
	same := len(used) == 2 && used[0] == used[1]
	mu.Unlock()
	if !same {
		t.Fatal("conversation switched accounts")
	}
	if _, err := service.SetEnabled(ctx, mapping[selected], false); err != nil {
		t.Fatal(err)
	}
	replacement, err := gateway.Open(ctx, 1, 1, raw, headers, Responses)
	if err != nil {
		t.Fatal("disabled conversation did not rebalance", err)
	}
	if replacement.entry.record.AccountID == mapping[selected] {
		t.Fatal("disabled account reused")
	}
	replacement.Body.Close()
	headers.Set("Session_id", "new-conversation")
	stream, err := gateway.Open(ctx, 1, 1, raw, headers, Responses)
	if err != nil {
		t.Fatal(err)
	}
	stream.Body.Close()
	releases := []func(){}
	for range cap(gateway.slots) {
		release, err := gateway.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if _, err := gateway.Acquire(); !errors.Is(err, ErrBusy) {
		t.Fatal("unbounded concurrent requests")
	}
	for _, release := range releases {
		release()
	}
}
