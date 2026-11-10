package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestQuotaWeightedSelectionKeepsBindings(t *testing.T) {
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	s.policy.QuotaWeighted = true
	ctx := context.Background()
	second, err := s.accounts.Authorize(ctx, "Synthetic second", accounts.Credential{AccountID: "synthetic-weighted", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO group_accounts(group_id,account_id) VALUES(1,?)", second.ID); err != nil {
		t.Fatal(err)
	}
	for id, used := range map[string]float64{ids["codex"]: 10, second.ID: 90} {
		value := upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{UsedPercent: &used}}}}}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO account_usage(account_id,snapshot,updated_at,revision) SELECT id,?,?,models_revision FROM accounts WHERE id=?`, raw, value.UpdatedAt, id); err != nil {
			t.Fatal(err)
		}
	}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		id, _, err := s.selectAccount(ctx, 1, 1, "", "codex", "", Responses)
		if err != nil {
			t.Fatal(err)
		}
		counts[id]++
	}
	if counts[ids["codex"]] != 90 || counts[second.ID] != 10 {
		t.Fatal("remaining quota was not weighted", counts)
	}
	bound, _, err := s.selectAccount(ctx, 1, 1, "synthetic-weighted-session", "codex", "", Responses)
	if err != nil {
		t.Fatal(err)
	}
	s.scheduler = smoothScheduler{}
	for i := 0; i < 5; i++ {
		id, _, err := s.selectAccount(ctx, 1, 1, "synthetic-weighted-session", "codex", "", Responses)
		if err != nil || id != bound {
			t.Fatal("weight changed affinity", id, err)
		}
	}
	if _, err := s.db.Exec("DELETE FROM account_usage WHERE account_id=?", second.ID); err != nil {
		t.Fatal(err)
	}
	counts = map[string]int{}
	s.scheduler = smoothScheduler{}
	for i := 0; i < 140; i++ {
		id, _, err := s.selectAccount(ctx, 1, 1, "", "codex", "", Responses)
		if err != nil {
			t.Fatal(err)
		}
		counts[id]++
	}
	if counts[ids["codex"]] != 90 || counts[second.ID] != 50 {
		t.Fatal("unknown quota must use its own conservative fallback", counts)
	}
}

func TestQuotaWeightUsesMostConstrainedFreshWindow(t *testing.T) {
	now := time.Now()
	low, high := 10.0, 90.0
	value := upstream.Usage{UpdatedAt: now.Unix(), Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{UsedPercent: &low}, {UsedPercent: &high}}}}}
	if got := quotaStatus(value, now).Weight; got != 10 {
		t.Fatalf("weight %d", got)
	}
	value.UpdatedAt -= 121
	if got := quotaStatus(value, now).Weight; got != 0 {
		t.Fatal("stale weight", got)
	}
	value.UpdatedAt = now.Unix()
	value.Limits[0].Windows[1].UsedPercent = nil
	if got := quotaStatus(value, now).Weight; got != 0 {
		t.Fatal("unknown weight", got)
	}
	value.Limits = append(value.Limits, upstream.UsageLimit{Windows: []upstream.UsageWindow{{UsedPercent: &low}}})
	if got := quotaStatus(value, now).Weight; got != 0 {
		t.Fatal("ambiguous main limits must retain equal rotation", got)
	}
}
