package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestProtocolPreferenceSelectsMatchingEligibleProvider(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected network")
		return nil, errors.New("synthetic")
	}))
	for _, tc := range []struct {
		kind     Kind
		provider string
	}{{Responses, "codex"}, {Chat, "codex"}, {Compact, "codex"}, {Messages, "claude"}, {Gemini, "antigravity"}, {GeminiStream, "antigravity"}} {
		for range 3 {
			id, _, err := s.selectAccount(context.Background(), 1, 1, "", "", "synthetic-model", tc.kind)
			if err != nil || id != ids[tc.provider] {
				t.Fatalf("kind %d chose %s instead of %s: %v", tc.kind, id, ids[tc.provider], err)
			}
		}
	}
	if id, _, err := s.selectAccount(context.Background(), 1, 1, "", "claude", "synthetic-model", Responses); err != nil || id != ids["claude"] {
		t.Fatal("explicit provider was overridden", id, err)
	}
}

func TestProtocolPreferenceHonorsAdmissionAndScope(t *testing.T) {
	for _, reason := range []string{"busy", "cooling", "quota", "model", "group", "policy"} {
		t.Run(reason, func(t *testing.T) {
			s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic") }))
			ctx := context.Background()
			if err := s.loadRuntime(ctx); err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "busy":
				s.health[ids["codex"]].InFlight = s.health[ids["codex"]].MaxConcurrency
			case "cooling":
				s.health[ids["codex"]].CooldownUntil = s.now().Add(time.Minute).Unix()
			case "quota":
				saveExhaustedQuota(t, s, ids["codex"])
			case "model":
				if err := s.accounts.SaveCatalog(ctx, ids["codex"], 0, []string{"other-model"}, s.now().Unix(), upstream.CatalogSource("codex")); err != nil {
					t.Fatal(err)
				}
			case "group", "policy":
				input := groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["claude"], ids["antigravity"]}}
				if reason == "policy" {
					input.AccountIDs = append(input.AccountIDs, ids["codex"])
					input.ModelPolicy = &groups.ModelPolicy{Restricted: true, Models: []string{"claude/synthetic-model"}}
				}
				if _, err := groups.New(s.db).Save(ctx, 1, input); err != nil {
					t.Fatal(err)
				}
			}
			id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses)
			if err != nil || id == ids["codex"] {
				t.Fatalf("preferred account bypassed %s: %s %v", reason, id, err)
			}
		})
	}
}

func TestProtocolPreferenceKeepsFallbackAffinityAfterRecoveryAndProtocolChange(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic") }))
	ctx := context.Background()
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	bound, _, err := s.selectAccount(ctx, 1, 1, "synthetic-session", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], true); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{Responses, Messages, Gemini} {
		id, _, err := s.selectAccount(ctx, 1, 1, "synthetic-session", "", "synthetic-model", kind)
		if err != nil || id != bound {
			t.Fatal("existing conversation moved", kind, id, err)
		}
	}
	if id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses); err != nil || id != ids["codex"] {
		t.Fatal("new request did not return to preferred provider", id, err)
	}
	s.health[bound].InFlight = s.health[bound].MaxConcurrency
	if id, _, err := s.selectAccount(ctx, 1, 1, "synthetic-session", "", "synthetic-model", Responses); err != nil || id != ids["codex"] {
		t.Fatal("busy binding did not use the preferred available account", id, err)
	}
}

func TestProtocolPreferenceRotatesWithinTierWithoutOtherProtocolInterference(t *testing.T) {
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic") }))
	ctx := context.Background()
	second, err := s.accounts.Authorize(ctx, "Synthetic second Codex", accounts.Credential{Provider: "codex", AccountID: "synthetic-second", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: s.now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.accounts.SaveCatalog(ctx, second.ID, 0, []string{"synthetic-model"}, s.now().Unix(), upstream.CatalogSource("codex")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO group_accounts(group_id,account_id) VALUES(1,?)", second.ID); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 4 {
		id, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Responses)
		if err != nil || (id != ids["codex"] && id != second.ID) {
			t.Fatal("non-preferred provider selected", id, err)
		}
		seen[id] = true
		if _, _, err := s.selectAccount(ctx, 1, 1, "", "", "synthetic-model", Messages); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 {
		t.Fatal("preferred pool rotation was starved", seen)
	}
}
