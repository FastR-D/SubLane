package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
	"net/http"
	"strings"
	"testing"
	"time"
)

func requestRoute(t *testing.T, s *Service, userID int64) map[string]any {
	t.Helper()
	page, err := s.requests(context.Background(), userID, RequestFilter{})
	if err != nil || len(page.Requests) == 0 {
		t.Fatal("missing request history", err)
	}
	raw, err := json.Marshal(page.Requests[0])
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	route, ok := record["route"].(map[string]any)
	if !ok {
		t.Fatalf("missing captured route explanation: %s", raw)
	}
	return route
}

func TestRouteExplanationPersistsSelectionWithoutExposingCandidates(t *testing.T) {
	s, ids := affinityGateway(t)
	x, err := s.Open(context.Background(), 1, 1, []byte(`{"model":"synthetic-codex","input":"Synthetic input"}`), nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Events(func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	x.Body.Close()
	route := requestRoute(t, s, 0)
	if route["strategy"] != "rotation" || route["result"] != "selected" || route["candidates"] != float64(1) {
		t.Fatalf("unexpected route: %+v", route)
	}
	excluded, ok := route["exclusions"].(map[string]any)
	if !ok || excluded["model"] != float64(2) {
		t.Fatalf("missing model exclusions: %+v", route)
	}
	if _, err := s.accounts.SetEnabled(context.Background(), ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	personal := requestRoute(t, s, 1)
	before, _ := json.Marshal(route)
	after, _ := json.Marshal(personal)
	if string(before) != string(after) {
		t.Fatalf("history reconstructed from changed state: %s != %s", before, after)
	}
	for _, id := range ids {
		if strings.Contains(string(after), id) {
			t.Fatal("candidate identity escaped into routing metadata")
		}
	}
	page, err := s.UserRequests(context.Background(), 1, RequestFilter{})
	if err != nil || page.Requests[0].AccountID != "" || page.Requests[0].AccountName != "" {
		t.Fatal("personal history exposed resource identity", err)
	}
}

func TestRouteExplanationRetainsConversationAndParentPreference(t *testing.T) {
	s, _, _ := reviewGateway(t)
	openReview(t, s, 1, 1, reviewHeaders("synthetic-child"))
	if got := requestRoute(t, s, 0)["strategy"]; got != "parent" {
		t.Fatalf("parent preference not captured: %v", got)
	}
	openReview(t, s, 1, 1, reviewHeaders("synthetic-child"))
	if got := requestRoute(t, s, 0)["strategy"]; got != "affinity" {
		t.Fatalf("authoritative conversation binding not captured: %v", got)
	}
}

func TestRouteExplanationLegacyRowsRemainUnavailable(t *testing.T) {
	s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("history must not contact upstream")
		return nil, nil
	}))
	if _, err := s.db.Exec("INSERT INTO request_records(user_id,key_id,group_id,model,transport,operation,started_at,duration_ms,outcome) VALUES(1,0,1,'synthetic-model','http','responses',?,0,'success')", s.now().Unix()); err != nil {
		t.Fatal(err)
	}
	page, err := s.Requests(context.Background(), RequestFilter{})
	if err != nil || len(page.Requests) != 1 {
		t.Fatal("legacy history unavailable", err)
	}
	raw, _ := json.Marshal(page.Requests[0])
	var record map[string]any
	json.Unmarshal(raw, &record)
	if value, exists := record["route"]; !exists || value != nil {
		t.Fatalf("legacy routing must be explicitly unavailable: %s", raw)
	}
}

func TestRouteExplanationRejectsTrailingOrIncompleteMetadata(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"strategy":"rotation","result":"selected"}`,
		`{"version":1,"strategy":"rotation","result":"selected","candidates":1,"parent_fallback":false,"exclusions":{}} false`,
	} {
		if got := decodeRoute(raw); got != nil {
			t.Fatalf("incompatible history must remain unavailable: %q", raw)
		}
	}
}

func TestRouteExplanationReflectsAdmissionFailureAfterSelection(t *testing.T) {
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("failed admission executed upstream")
		return nil, nil
	}))
	row, err := s.queries.GetAccountModelRuntime(context.Background(), db.GetAccountModelRuntimeParams{TargetID: ids["codex"], WorkspaceID: 1, Model: "synthetic-model"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxModelRuntime; i++ {
		key := modelKey{ids["codex"], fmt.Sprintf("synthetic-held-%d", i)}
		s.modelHealth[key] = &modelRuntime{key: key, lifecycle: row.Lifecycle, runtimeRevision: row.RuntimeRevision}
	}
	if _, err := s.Open(context.Background(), 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic input"}`), nil, Responses); !errors.Is(err, ErrBusy) {
		t.Fatal("expected model lease capacity failure", err)
	}
	route := requestRoute(t, s, 0)
	if route["result"] != "rejected" {
		t.Fatalf("successful selection must not fabricate successful admission: %+v", route)
	}
}

func TestRouteExplanationCapturesRejectionGroupsAndParentFallback(t *testing.T) {
	for _, reason := range []string{"quota", "cooldown", "model cooldown", "concurrency", "unavailable", "policy", "model"} {
		t.Run(reason, func(t *testing.T) {
			s, parent, _ := reviewGateway(t)
			switch reason {
			case "quota":
				saveExhaustedQuota(t, s, parent)
			case "cooldown":
				s.health[parent].CooldownUntil = s.now().Add(time.Minute).Unix()
			case "model cooldown":
				row, err := s.queries.GetAccountModelRuntime(context.Background(), db.GetAccountModelRuntimeParams{TargetID: parent, WorkspaceID: 1, Model: "codex-auto-review"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.queries.SaveAccountModelRuntime(context.Background(), db.SaveAccountModelRuntimeParams{TargetID: parent, WorkspaceID: 1, Model: "codex-auto-review", Lifecycle: row.Lifecycle, RuntimeRevision: row.RuntimeRevision, CooldownUntil: s.now().Add(time.Minute).Unix(), Failures: 1, Reason: "synthetic"}); err != nil {
					t.Fatal(err)
				}
			case "concurrency":
				s.health[parent].InFlight = s.health[parent].MaxConcurrency
				defer func() { s.health[parent].InFlight = 0 }()
			case "unavailable":
				if _, err := s.accounts.SetEnabled(context.Background(), parent, false); err != nil {
					t.Fatal(err)
				}
			case "policy":
				if _, err := s.db.Exec("UPDATE accounts SET provider='claude',account_id='synthetic-policy-resource' WHERE id=?", parent); err != nil {
					t.Fatal(err)
				}
			case "model":
				if err := s.accounts.SaveCatalog(context.Background(), parent, 0, []string{"synthetic-model"}, s.now().Unix(), upstream.CatalogSource("codex")); err != nil {
					t.Fatal(err)
				}
			}
			headers := reviewHeaders("synthetic-fallback")
			raw := []byte(`{"model":"codex/codex-auto-review","input":"Synthetic input"}`)
			x, err := s.Open(context.Background(), 1, 1, raw, headers, Responses)
			if err != nil {
				t.Fatal(err)
			}
			if err := x.Events(func([]byte) error { return nil }); err != nil {
				t.Fatal(err)
			}
			x.Body.Close()
			route := requestRoute(t, s, 0)
			exclusions := route["exclusions"].(map[string]any)
			if reason == "model cooldown" {
				reason = "cooldown"
			}
			if exclusions[reason] != float64(1) || route["parent_fallback"] != true || route["strategy"] != "rotation" {
				t.Fatalf("fallback reason missing: %+v", route)
			}
		})
	}
}

func TestRouteExplanationCapturesNoSelectionAndBlockedBinding(t *testing.T) {
	for _, session := range []string{"", "synthetic-bound"} {
		t.Run(session, func(t *testing.T) {
			s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("exhausted account executed upstream")
				return nil, nil
			}))
			if session != "" {
				if _, _, err := s.selectAccount(context.Background(), 1, 1, session, "", "synthetic-model", Responses); err != nil {
					t.Fatal(err)
				}
			}
			saveExhaustedQuota(t, s, ids["codex"])
			headers := http.Header{"Session_id": {session}}
			if _, err := s.Open(context.Background(), 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic input"}`), headers, Responses); !errors.Is(err, ErrQuotaExhausted) {
				t.Fatal(err)
			}
			route := requestRoute(t, s, 0)
			strategy := "none"
			if session != "" {
				strategy = "affinity"
			}
			if route["strategy"] != strategy || route["result"] != "rejected" || route["exclusions"].(map[string]any)["quota"] != float64(1) {
				t.Fatalf("quota rejection not captured: %+v", route)
			}
		})
	}
}

func TestRouteExplanationRetainsWeightingWithUnknownCandidate(t *testing.T) {
	s, parent, other := reviewGateway(t)
	s.policy.QuotaWeighted = true
	for _, id := range []string{parent, other} {
		saveProviderQuota(t, s, id, upstream.Usage{UpdatedAt: s.now().Unix(), Limits: []upstream.UsageLimit{{Windows: []upstream.UsageWindow{{UsedPercent: quotaFloat(20)}}}}})
	}
	for i, want := range []string{"quota_weighted", "quota_weighted"} {
		if i == 1 {
			if _, err := s.db.Exec("DELETE FROM account_usage WHERE account_id=?", other); err != nil {
				t.Fatal(err)
			}
		}
		x, err := s.Open(context.Background(), 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic input"}`), nil, Responses)
		if err != nil {
			t.Fatal(err)
		}
		if err := x.Events(func([]byte) error { return nil }); err != nil {
			t.Fatal(err)
		}
		x.Body.Close()
		if got := requestRoute(t, s, 0)["strategy"]; got != want {
			t.Fatalf("actual strategy=%v, want %s", got, want)
		}
	}
}

func TestRouteExplanationRejectsNullOrContradictoryFields(t *testing.T) {
	valid := `{"version":1,"strategy":"rotation","result":"selected","candidates":1,"parent_fallback":false,"exclusions":{"model":0,"policy":0,"quota":0,"cooldown":0,"concurrency":0,"unavailable":0}}`
	for _, raw := range []string{
		strings.Replace(valid, `"candidates":1`, `"candidates":null`, 1),
		strings.Replace(valid, `"parent_fallback":false`, `"parent_fallback":null`, 1),
		strings.Replace(valid, `"model":0`, `"model":null`, 1),
		strings.Replace(valid, `"strategy":"rotation"`, `"strategy":"none"`, 1),
		strings.Replace(valid, `"candidates":1`, `"candidates":0`, 1),
	} {
		if got := decodeRoute(raw); got != nil {
			t.Fatalf("invalid history must remain unavailable: %s", raw)
		}
	}
}
