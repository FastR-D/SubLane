package pricing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestListReturnsSortedIndependentPrices(t *testing.T) {
	s := NewStatic(map[string]Price{
		"synthetic-zeta":  {Input: 2000000, Cached: 200000, Output: 8000000, Source: "override"},
		"synthetic-alpha": {Input: 1250000, Cached: 125000, Output: 10000000, Source: "remote"},
	})
	list := s.List()
	if len(list) != 2 || list[0].Model != "synthetic-alpha" || list[1].Model != "synthetic-zeta" {
		t.Fatalf("unexpected catalog: %+v", list)
	}
	if list[0].Input != 1250000 || list[0].Cached != 125000 || list[0].Output != 10000000 || list[0].Source != "remote" {
		t.Fatalf("changed price units or source: %+v", list[0])
	}
	list[0].Input = 1
	if price, _ := s.Lookup("synthetic-alpha"); price.Input != 1250000 {
		t.Fatal("listing mutated the shared catalog")
	}
	if got := NewStatic(nil).List(); got == nil || len(got) != 0 {
		t.Fatalf("empty catalog must be a JSON array: %+v", got)
	}
}

func TestParseLiteLLMPriceAndModelAliases(t *testing.T) {
	prices, err := parsePrices([]byte(`{"gpt-5.1-codex":{"input_cost_per_token":0.00000125,"cache_read_input_token_cost":0.000000125,"output_cost_per_token":0.00001}}`), "remote")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{prices: prices}
	got, ok := s.Lookup("codex/gpt-5.1-codex(high)")
	if !ok || got.Input != 1250000 || got.Cached != 125000 || got.Output != 10000000 {
		t.Fatalf("unexpected price: %+v, %v", got, ok)
	}
}

func TestRefreshUsesHashAndKeepsCacheOnFailure(t *testing.T) {
	var jsonCalls atomic.Int32
	document := []byte(`{"gpt-5.1-codex":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	var hash = hashDocument(document)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hash" {
			_, _ = fmt.Fprintln(w, hash)
			return
		}
		jsonCalls.Add(1)
		if hash == "broken" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(document)
	}))
	defer server.Close()

	dir := t.TempDir()
	s, err := New(Config{CachePath: filepath.Join(dir, "prices.json"), HashPath: filepath.Join(dir, "prices.sha256"), URL: server.URL, HashURL: server.URL + "/hash"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if jsonCalls.Load() != 1 {
		t.Fatalf("first refresh downloaded %d times", jsonCalls.Load())
	}
	if err = s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if jsonCalls.Load() != 1 {
		t.Fatalf("unchanged hash downloaded %d times", jsonCalls.Load())
	}
	hash = "broken"
	if err = s.Refresh(context.Background()); err == nil {
		t.Fatal("expected failed download")
	}
	if got, ok := s.Lookup("gpt-5.1-codex"); !ok || got.Input != 1000000 {
		t.Fatalf("cache was discarded: %+v, %v", got, ok)
	}
}

func TestRefreshRejectsMismatchedDocumentHash(t *testing.T) {
	document := []byte(`{"synthetic-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hash" {
			_, _ = fmt.Fprintln(w, hashDocument([]byte("different document")))
			return
		}
		_, _ = w.Write(document)
	}))
	defer server.Close()
	s, err := New(Config{URL: server.URL, HashURL: server.URL + "/hash"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("accepted a document that did not match its SHA-256")
	}
	if _, ok := s.Lookup("synthetic-model"); ok {
		t.Fatal("published mismatched prices")
	}
}

func TestCustomPriceURLWithoutHashURL(t *testing.T) {
	document := []byte(`{"synthetic-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(document)
	}))
	defer server.Close()
	s, err := New(Config{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("custom document without matching hash URL: %v", err)
	}
}

func TestMismatchedCachedDocumentDoesNotSkipRefresh(t *testing.T) {
	valid := []byte(`{"synthetic-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	corrupt := []byte(`{"synthetic-model":{"input_cost_per_token":0.000009,"output_cost_per_token":0.000009}}`)
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hash" {
			_, _ = fmt.Fprintln(w, hashDocument(valid))
			return
		}
		downloads.Add(1)
		_, _ = w.Write(valid)
	}))
	defer server.Close()
	dir := t.TempDir()
	cache := filepath.Join(dir, "prices.json")
	hashPath := filepath.Join(dir, "prices.sha256")
	if err := os.WriteFile(cache, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hashPath, []byte(hashDocument(valid)), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{URL: server.URL, HashURL: server.URL + "/hash", CachePath: cache, HashPath: hashPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup("synthetic-model"); ok {
		t.Fatal("published cached prices with a mismatched hash")
	}
	if err := s.Refresh(context.Background()); err != nil || downloads.Load() != 1 {
		t.Fatalf("refresh did not replace corrupted cache: downloads=%d err=%v", downloads.Load(), err)
	}
}

func TestRefreshDoesNotPublishWhenCacheWriteFails(t *testing.T) {
	document := []byte(`{"synthetic-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hash" {
			_, _ = fmt.Fprintln(w, hashDocument(document))
			return
		}
		_, _ = w.Write(document)
	}))
	defer server.Close()
	s, err := New(Config{URL: server.URL, HashURL: server.URL + "/hash", CachePath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected cache write failure")
	}
	if _, ok := s.Lookup("synthetic-model"); ok {
		t.Fatal("published prices that were not persisted")
	}
}

func TestFallbackAndOverride(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "fallback.json")
	override := filepath.Join(dir, "override.json")
	if err := os.WriteFile(fallback, []byte(`{"claude-3-7-sonnet-20250219":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000004}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte(`{"claude-3-7-sonnet-20250219":{"output_cost_per_token":0.000005}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{FallbackPath: fallback, OverridePath: override})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.Lookup("claude-3-7-sonnet-20250219")
	if !ok || got.Input != 3000000 || got.Output != 5000000 {
		t.Fatalf("fallback/override merge failed: %+v, %v", got, ok)
	}
}

func TestHashForDocument(t *testing.T) {
	if got := hashDocument([]byte("synthetic")); got != "b3cc0475bb78a5026098858e9889acf666d31062d513d303314eca31d36e72f2" {
		t.Fatalf("unexpected hash: %s", got)
	}
}

func TestImageGenerationPricesUseImageOutputTokens(t *testing.T) {
	// The default feed lists both text and image output rates for gpt-image models.
	prices, err := parsePrices([]byte(`{"gpt-image-2":{"mode":"image_generation","input_cost_per_token":0.000005,"cache_read_input_token_cost":0.00000125,"output_cost_per_token":0.00001,"input_cost_per_image_token":0.000008,"output_cost_per_image_token":0.00003},"image-without-mode":{"input_cost_per_token":0.000005,"output_cost_per_token":0.00001,"input_cost_per_image_token":0.000008,"output_cost_per_image_token":0.00003}}`), "remote")
	if err != nil {
		t.Fatal(err)
	}
	price, ok := prices["gpt-image-2"]
	if !ok || price.Input != 5_000_000 || price.Cached != 1_250_000 || price.Output != 30_000_000 || price.ImageInput != 8_000_000 {
		t.Fatalf("image price: %+v %v", price, ok)
	}
	if other := prices["image-without-mode"]; other.Output != 10_000_000 || other.ImageInput != 0 {
		t.Fatalf("image token prices were applied outside image generation models: %+v", other)
	}
}
