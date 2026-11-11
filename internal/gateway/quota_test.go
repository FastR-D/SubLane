package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestQuotaDecisionOnlyUsesFreshMainLimits(t *testing.T) {
	now := time.Now().Unix()
	for _, test := range []struct {
		name, raw string
		blocked   bool
	}{
		{"exhausted", `{"limits":[{"name":"","windows":[{"used_percent":100,"reset_at":RESET}]}]}`, true},
		{"remaining", `{"limits":[{"name":"","windows":[{"used_percent":99}]}]}`, false},
		{"explicit denial", `{"limits":[{"name":"","allowed":false}]}`, true},
		{"explicit reached", `{"limits":[{"name":"","limit_reached":true}]}`, true},
		{"additional limit", `{"limits":[{"name":"synthetic-special","allowed":false}]}`, false},
		{"unknown", `{"limits":[{"name":"","windows":[]}]}`, false},
		{"already reset", `{"limits":[{"name":"","allowed":false,"windows":[{"used_percent":100,"reset_at":PAST}]}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := stringsReplaceQuotaTime(test.raw, now)
			var usage upstream.Usage
			if err := json.Unmarshal([]byte(raw), &usage); err != nil {
				t.Fatal(err)
			}
			usage.UpdatedAt = now - 1
			got := quotaStatus(usage, time.Unix(now, 0))
			if (got.State == "exhausted") != test.blocked {
				t.Fatalf("unexpected decision: %+v", got)
			}
			usage.UpdatedAt = now - 121
			if quotaStatus(usage, time.Unix(now, 0)).State == "exhausted" {
				t.Fatal("stale snapshot blocked admission")
			}
			usage.UpdatedAt = now + 10
			if quotaStatus(usage, time.Unix(now, 0)).State == "exhausted" {
				t.Fatal("future snapshot blocked admission")
			}
		})
	}
}

func TestQuotaSelectionPersistsAndRebalancesStickyConversation(t *testing.T) {
	ctx := context.Background()
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	first, _, err := s.selectAccount(ctx, 1, 1, "synthetic-sticky", "codex", "", Responses)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.accounts.Authorize(ctx, "Synthetic second", accounts.Credential{AccountID: "quota-second", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO group_accounts(group_id,account_id) VALUES(1,?)", second.ID); err != nil {
		t.Fatal(err)
	}
	saveExhaustedQuota(t, s, ids["codex"])
	restarted := New(ctx, s.db, s.accounts, s.provider)
	defer restarted.Close()
	chosen, _, err := restarted.selectAccount(ctx, 1, 1, "", "codex", "", Responses)
	if err != nil || chosen != second.ID {
		t.Fatal("exhausted account was selected", chosen, err)
	}
	chosen, _, err = restarted.selectAccount(ctx, 1, 1, "synthetic-sticky", "codex", "", Responses)
	if err != nil || chosen != second.ID {
		t.Fatal("sticky quota did not rebalance", chosen, err)
	}
	saveExhaustedQuota(t, s, second.ID)
	if _, _, err := restarted.selectAccount(ctx, 1, 1, "", "codex", "", Responses); !errors.Is(err, ErrQuotaExhausted) {
		t.Fatal("pool quota not reported", err)
	}
	// Disabling and enabling invalidates observations from the older authorization lifecycle.
	if _, err := s.accounts.SetEnabled(ctx, first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.accounts.SetEnabled(ctx, first, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.selectAccount(ctx, 1, 1, "synthetic-sticky", "codex", "", Responses); err != nil {
		t.Fatal("obsolete quota survived lifecycle change", err)
	}
}

func stringsReplaceQuotaTime(raw string, now int64) string {
	return strings.NewReplacer("RESET", strconv.FormatInt(now+60, 10), "PAST", strconv.FormatInt(now-1, 10)).Replace(raw)
}
func saveExhaustedQuota(t *testing.T, s *Service, id string) {
	t.Helper()
	now := s.now().Unix()
	raw := stringsReplaceQuotaTime(`{"updated_at":NOW,"limits":[{"name":"","limit_reached":true,"windows":[{"used_percent":100,"reset_at":RESET}]}]}`, now)
	raw = strings.ReplaceAll(raw, "NOW", strconv.FormatInt(now, 10))
	if _, err := s.db.Exec(`INSERT INTO account_usage(account_id,snapshot,updated_at,revision) SELECT id,?, ?, models_revision FROM accounts WHERE id=? ON CONFLICT(account_id) DO UPDATE SET snapshot=excluded.snapshot,updated_at=excluded.updated_at,revision=excluded.revision`, []byte(raw), now, id); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaRejectionIsRecordedWithoutPenalizingAccount(t *testing.T) {
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("exhausted account executed a model")
		return syntheticStream(), nil
	}))
	saveExhaustedQuota(t, s, ids["codex"])
	if _, err := s.Open(context.Background(), 1, 1, []byte(`{"model":"synthetic-model","input":"synthetic"}`), nil, Responses); !errors.Is(err, ErrQuotaExhausted) {
		t.Fatal(err)
	}
	page, err := s.Requests(context.Background(), RequestFilter{})
	if err != nil || len(page.Requests) != 1 || page.Requests[0].ErrorCode != "quota_exhausted" || page.Requests[0].Outcome != "rejected" {
		t.Fatal("quota rejection missing", page, err)
	}
	states, err := s.Runtime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.ID == ids["codex"] && (state.State != "quota_exhausted" || state.Failures != 0 || state.InFlight != 0) {
			t.Fatal("quota conflated with transient failure", state)
		}
	}
}

func TestSubscriptionQuotaSelectionPreservesAffinityAndModelScope(t *testing.T) {
	for _, provider := range []string{"claude", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			ctx := context.Background()
			s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
			id := ids[provider]
			catalog, err := s.accounts.Catalog(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.accounts.SaveCatalog(ctx, id, catalog.Revision, []string{"synthetic-model", "synthetic-other"}, s.now().Unix(), upstream.CatalogSource(provider)); err != nil {
				t.Fatal(err)
			}
			bound, _, err := s.selectAccount(ctx, 1, 1, "synthetic-conversation", provider, "synthetic-model", Responses)
			if err != nil || bound != id {
				t.Fatal("initial binding", bound, err)
			}
			snapshot := upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Name: "", Windows: []upstream.UsageWindow{{Kind: "primary", UsedPercent: quotaFloat(100)}}}}}
			if provider == "antigravity" {
				snapshot.Limits[0].Name = "synthetic-model"
				snapshot.Limits[0].Windows[0].Kind = "model"
			}
			saveProviderQuota(t, s, id, snapshot)
			restarted := New(ctx, s.db, s.accounts, s.provider)
			defer restarted.Close()
			if chosen, _, err := restarted.selectAccount(ctx, 1, 1, "synthetic-conversation", provider, "synthetic-model", Responses); !errors.Is(err, ErrQuotaExhausted) || chosen != id {
				t.Fatal("exhausted binding moved or was admitted", chosen, err)
			}
			if _, _, err := restarted.selectAccount(ctx, 1, 1, "", provider, "synthetic-model", Responses); !errors.Is(err, ErrQuotaExhausted) {
				t.Fatal("exhausted model selected", err)
			}
			_, _, err = restarted.selectAccount(ctx, 1, 1, "", provider, "synthetic-other", Responses)
			if provider == "antigravity" && err != nil || provider == "claude" && !errors.Is(err, ErrQuotaExhausted) {
				t.Fatal("wrong quota scope", err)
			}
			states, err := restarted.Runtime(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, state := range states {
				if state.ID == id && (state.Failures != 0 || provider == "antigravity" && state.State == "quota_exhausted" || provider == "claude" && state.State != "quota_exhausted") {
					t.Fatal("model quota became an account-wide failure", state)
				}
			}
			if _, err := s.accounts.SetEnabled(ctx, id, false); err != nil {
				t.Fatal(err)
			}
			if _, err := s.accounts.SetEnabled(ctx, id, true); err != nil {
				t.Fatal(err)
			}
			if _, _, err := restarted.selectAccount(ctx, 1, 1, "synthetic-conversation", provider, "synthetic-model", Responses); err != nil {
				t.Fatal("obsolete quota survived lifecycle", err)
			}
		})
	}
}

func TestSubscriptionQuotaNeverInventsExhaustion(t *testing.T) {
	ctx := context.Background()
	s, ids := providerGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	now := s.now().Unix()
	for _, provider := range []string{"claude", "antigravity"} {
		for _, test := range []struct {
			name      string
			updated   int64
			used      *float64
			reset     *int64
			limitName string
		}{
			{"stale", now - 121, quotaFloat(100), nil, ""},
			{"future", now + 10, quotaFloat(100), nil, ""},
			{"already reset", now, quotaFloat(100), quotaInt(now - 1), ""},
			{"unknown percentage", now, nil, nil, ""},
			{"remaining", now, quotaFloat(99), nil, ""},
			{"unrelated named limit", now, quotaFloat(100), nil, "synthetic-unrelated"},
		} {
			t.Run(provider+"/"+test.name, func(t *testing.T) {
				name := test.limitName
				if provider == "antigravity" && name == "" {
					name = "synthetic-model"
				}
				saveProviderQuota(t, s, ids[provider], upstream.Usage{UpdatedAt: test.updated, Limits: []upstream.UsageLimit{{Name: name, Windows: []upstream.UsageWindow{{UsedPercent: test.used, ResetAt: test.reset}}}}})
				if _, _, err := s.selectAccount(ctx, 1, 1, "", provider, "synthetic-model", Responses); err != nil {
					t.Fatal("unusable or unrelated observation blocked selection", err)
				}
			})
		}
	}
}

func saveProviderQuota(t *testing.T, s *Service, id string, usage upstream.Usage) {
	t.Helper()
	raw, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO account_usage(account_id,snapshot,updated_at,revision) SELECT id,?,?,models_revision FROM accounts WHERE id=? ON CONFLICT(account_id) DO UPDATE SET snapshot=excluded.snapshot,updated_at=excluded.updated_at,revision=excluded.revision`, raw, usage.UpdatedAt, id); err != nil {
		t.Fatal(err)
	}
}
func quotaFloat(value float64) *float64 { return &value }
func quotaInt(value int64) *int64       { return &value }
