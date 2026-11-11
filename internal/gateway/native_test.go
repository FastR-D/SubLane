package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestNativeCatalogDeduplicatesAcrossProvidersAndHonorsScopedRules(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, errors.New("synthetic")
	}))
	ctx := context.Background()
	models, err := s.Models(ctx, 1, 1)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-model" || models[0].OwnedBy != "sublane" {
		t.Fatal(models, err)
	}
	input := groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"], ids["claude"], ids["antigravity"]}, ModelPolicy: &groups.ModelPolicy{Restricted: true, Models: []string{"claude/synthetic-model"}}}
	if _, err := groups.New(s.db).Save(ctx, 1, input); err != nil {
		t.Fatal(err)
	}
	models, err = s.Models(ctx, 1, 1)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-model" || models[0].OwnedBy != "claude" {
		t.Fatal(models, err)
	}
	id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses)
	if err != nil || id != ids["claude"] {
		t.Fatal("native request escaped scoped policy", id, err)
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "", "codex", "synthetic-model", Responses); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatal("legacy alias escaped scoped policy", err)
	}
}

func TestNativeRoutingRotatesFallbacksAndKeepsItsAccountAcrossRestart(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, errors.New("synthetic")
	}))
	ctx := context.Background()
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 3 {
		id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses)
		if err != nil {
			t.Fatal(err)
		}
		seen[id] = true
	}
	if len(seen) != 2 {
		t.Fatal("native routing did not rotate available fallback providers", seen)
	}
	id, _, err := s.selectAccount(ctx, 1, 1, "synthetic-auto-session", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	reopened := New(ctx, s.db, s.accounts, s.provider)
	defer reopened.Close()
	again, _, err := reopened.selectAccount(ctx, 1, 1, "synthetic-auto-session", "", "synthetic-model", Responses)
	if err != nil || again != id {
		t.Fatal("automatic binding was lost", err)
	}
	if _, err := s.accounts.SetEnabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if next, _, err := reopened.selectAccount(ctx, 1, 1, "synthetic-auto-session", "", "synthetic-model", Responses); err != nil || next == id {
		t.Fatal("disabled automatic binding did not rebalance", next, err)
	}
}

func TestNativeRoutingAdoptsLegacyAffinityWithoutMovingIt(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic") }))
	ctx := context.Background()
	id, _, err := s.selectAccount(ctx, 1, 1, "synthetic-legacy", "codex", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, err := s.selectAccount(ctx, 1, 1, "synthetic-legacy", "", "synthetic-model", Responses)
	if err != nil || actual != id {
		t.Fatal("legacy binding moved", err)
	}
	for _, provider := range []string{"codex", "claude"} {
		if _, _, err := s.selectAccount(ctx, 1, 1, "synthetic-ambiguous", provider, "synthetic-model", Responses); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "synthetic-ambiguous", "", "synthetic-model", Responses); !errors.Is(err, ErrAffinityUnavailable) {
		t.Fatal("ambiguous legacy conversation moved", err)
	}
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	if next, _, err := s.selectAccount(ctx, 1, 1, "synthetic-legacy", "", "synthetic-model", Responses); err != nil || next == id {
		t.Fatal("disabled legacy account did not rebalance", next, err)
	}
}

func TestNativeSelectionRebalancesBusyBoundSessions(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic") }))
	ctx := context.Background()
	first, _, err := s.selectAccount(ctx, 1, 1, "synthetic-busy-session", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	s.health[first].InFlight = s.health[first].MaxConcurrency
	next, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses)
	if err != nil || next == first {
		t.Fatal("new request chose busy account", next, err)
	}
	if next, _, err := s.selectAccount(ctx, 1, 1, "synthetic-busy-session", "", "synthetic-model", Responses); err != nil || next == first {
		t.Fatal("busy conversation did not rebalance", next, err)
	}
	s.health[first].InFlight = 0
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	bound, _, err := s.selectAccount(ctx, 1, 1, "synthetic-noncodex", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], true); err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.selectAccount(ctx, 1, 1, "synthetic-noncodex", "", "synthetic-model", Compact); !errors.Is(err, upstream.ErrInput) || got != bound {
		t.Fatal("compaction moved provider", got, err)
	}
	got, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Compact)
	if err != nil || got != ids["codex"] {
		t.Fatal("new compaction selected non-Codex account", got, err)
	}
}

func TestNativeSelectionPreservesScopedPolicyAfterBinding(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic") }))
	ctx := context.Background()
	if _, _, err := s.selectAccount(ctx, 1, 1, "synthetic-scoped", "claude", "synthetic-model", Responses); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "synthetic-scoped", "", "synthetic-model", Responses); err != nil {
		t.Fatal(err)
	}
	input := groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"], ids["claude"], ids["antigravity"]}, ModelPolicy: &groups.ModelPolicy{Restricted: true, Models: []string{"codex/synthetic-model"}}}
	if _, err := groups.New(s.db).Save(ctx, 1, input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.selectAccount(ctx, 1, 1, "synthetic-scoped", "", "synthetic-model", Responses); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatal("bound provider bypassed tightened policy", err)
	}
}
