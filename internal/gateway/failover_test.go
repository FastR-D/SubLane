package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

const failoverSession = "synthetic-failover"

func failoverGateway(t *testing.T, transport transportFunc) (*Service, string, string) {
	t.Helper()
	s, ids := codexGateway(t, transport)
	ctx := t.Context()
	if _, _, err := s.selectAccount(ctx, 1, 1, failoverSession, "", "synthetic-model", Responses); err != nil {
		t.Fatal(err)
	}
	backup, err := s.accounts.Authorize(ctx, "Synthetic backup", accounts.Credential{Provider: "codex", AccountID: "synthetic-backup", AccessToken: "synthetic-backup-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.accounts.SaveCatalog(ctx, backup.ID, 0, []string{"synthetic-model"}, s.now().Unix(), upstream.CatalogSource("codex")); err != nil {
		t.Fatal(err)
	}
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{ids["codex"], backup.ID}}); err != nil {
		t.Fatal(err)
	}
	return s, ids["codex"], backup.ID
}

func TestUnavailableAffinityRebalancesWithoutChangingContext(t *testing.T) {
	for _, reason := range []string{"disabled", "reauthorization", "account cooldown", "model cooldown", "concurrency", "quota"} {
		t.Run(reason, func(t *testing.T) {
			var forwarded map[string]json.RawMessage
			var authorization string
			s, primary, backup := failoverGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(r.Body).Decode(&forwarded); err != nil {
					t.Error(err)
				}
				authorization = r.Header.Get("Authorization")
				return syntheticStream(), nil
			}))
			ctx := t.Context()
			if err := s.loadRuntime(ctx); err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "disabled":
				_, err := s.accounts.SetEnabled(ctx, primary, false)
				if err != nil {
					t.Fatal(err)
				}
			case "reauthorization":
				if _, err := s.db.Exec("UPDATE accounts SET status='reauth_required' WHERE id=?", primary); err != nil {
					t.Fatal(err)
				}
			case "account cooldown":
				s.health[primary].CooldownUntil = s.now().Add(time.Minute).Unix()
			case "model cooldown":
				state, err := s.modelLease(ctx, primary, "synthetic-model")
				if err != nil {
					t.Fatal(err)
				}
				state.cooldownUntil = s.now().Add(time.Minute).Unix()
				s.modelHealth[state.key] = state
			case "concurrency":
				s.health[primary].InFlight = s.health[primary].MaxConcurrency
			case "quota":
				saveExhaustedQuota(t, s, primary)
			}
			const raw = `{"model":"synthetic-model","instructions":"Synthetic instructions","input":[{"type":"compaction","encrypted_content":"synthetic-compaction"},{"type":"reasoning","encrypted_content":"gAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==","summary":[]},{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Synthetic prior reply"}]},{"type":"function_call","call_id":"call_synthetic","name":"synthetic","arguments":"{}"},{"type":"function_call_output","call_id":"call_synthetic","output":"Synthetic result"},{"role":"user","content":"Continue with complete history"}]} `
			x, err := s.Open(ctx, 1, 1, []byte(raw), http.Header{"Session_id": {failoverSession}}, Responses)
			if err != nil {
				t.Fatal("unavailable binding did not rebalance", err)
			}
			defer x.Body.Close()
			if err := x.Events(func([]byte) error { return nil }); err != nil {
				t.Fatal(err)
			}
			x.Body.Close()
			if authorization != "Bearer synthetic-backup-access" {
				t.Fatal("request did not use the replacement credential", authorization)
			}
			var original map[string]json.RawMessage
			json.Unmarshal([]byte(raw), &original)
			for _, key := range []string{"input", "instructions"} {
				var want, got any
				json.Unmarshal(original[key], &want)
				json.Unmarshal(forwarded[key], &got)
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("failover changed context %s: want %#v, got %#v", key, want, got)
				}
			}
			reopened := New(ctx, s.db, s.accounts, s.provider)
			defer reopened.Close()
			if id, _, err := reopened.selectAccount(ctx, 1, 1, failoverSession, "", "synthetic-model", Responses); err != nil || id != backup {
				t.Fatal("replacement binding did not survive restart", id, err)
			}
			var count int
			if err := s.db.QueryRow("SELECT count(*) FROM account_affinity").Scan(&count); err != nil || count != 1 {
				t.Fatal("failover duplicated the binding", count, err)
			}
		})
	}
}

func TestFailedStreamRetryRebalancesAfterRestart(t *testing.T) {
	s, primary, backup := failoverGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") == "Bearer synthetic-backup-access" {
			return syntheticStream(), nil
		}
		body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_synthetic\"}}\n\n" +
			"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_synthetic\",\"error\":{\"code\":\"server_is_overloaded\",\"type\":\"service_unavailable_error\",\"message\":\"Synthetic overload\"}}}\n\n"
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	ctx := t.Context()
	raw := []byte(`{"model":"synthetic-model","input":"Synthetic complete input"}`)
	headers := http.Header{"Session_id": {failoverSession}}
	x, err := s.Open(ctx, 1, 1, raw, headers, Responses)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Events(func([]byte) error { return nil }); err == nil {
		t.Fatal("failed stream was reported as success")
	}
	x.Body.Close()
	reopened := New(ctx, s.db, s.accounts, s.provider)
	defer reopened.Close()
	retry, err := reopened.Open(ctx, 1, 1, raw, headers, Responses)
	if err != nil {
		t.Fatal("client retry failed", err)
	}
	defer retry.Body.Close()
	if retry.entry.record.AccountID != backup || retry.entry.record.AccountID == primary {
		t.Fatal("retry reused failed account", retry.entry.record.AccountID)
	}
}

func TestBusyPoolFailsImmediatelyWithoutLeakingAdmission(t *testing.T) {
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	if err := s.SetConcurrency(t.Context(), ids["codex"], 1); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"model":"synthetic-model","input":"Synthetic input"}`)
	first, err := s.Open(t.Context(), 1, 1, raw, nil, Responses)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	// The first lease stays held until this call returns, so queued admission cannot succeed.
	// Allow race-instrumented SQLite checks time to complete on a loaded test host.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	x, err := s.Open(ctx, 1, 1, raw, nil, Responses)
	if x != nil {
		x.Body.Close()
	}
	if !errors.Is(err, ErrAccountBusy) {
		t.Fatal("busy pool waited instead of rejecting immediately", err)
	}
	if s.memberActive[1] != 1 || s.health[ids["codex"]].InFlight != 1 {
		t.Fatal("rejected request leaked admission")
	}
}

func TestConcurrentFailoverRetainsOneReplacementBinding(t *testing.T) {
	s, primary, backup := failoverGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	if _, err := s.accounts.SetEnabled(t.Context(), primary, false); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			x, err := s.Open(t.Context(), 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic input"}`), http.Header{"Session_id": {failoverSession}}, Responses)
			if err != nil {
				t.Error(err)
				return
			}
			defer x.Body.Close()
			if x.entry.record.AccountID != backup {
				t.Error("concurrent request diverged from replacement")
			}
			if err := x.Events(func([]byte) error { return nil }); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
}

func TestFailoverDoesNotEscapePoolOrRewriteBindingWhenUnavailable(t *testing.T) {
	s, primary, backup := failoverGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unavailable pool reached upstream")
		return syntheticStream(), nil
	}))
	ctx := t.Context()
	if _, err := s.accounts.SetEnabled(ctx, primary, false); err != nil {
		t.Fatal(err)
	}
	// A healthy account outside the key's pool must never become a fallback.
	if _, err := s.db.Exec("DELETE FROM group_accounts WHERE account_id=?", backup); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic input"}`), http.Header{"Session_id": {failoverSession}}, Responses); !errors.Is(err, ErrNoAccount) {
		t.Fatal(err)
	}
	var bound string
	if err := s.db.QueryRow("SELECT account_id FROM account_affinity").Scan(&bound); err != nil || bound != primary {
		t.Fatal("failed selection rewrote binding", bound, err)
	}
	if s.memberActive[1] != 0 {
		t.Fatal("failed selection leaked member admission")
	}
}

func TestFailoverSettlesOneAllowanceEntryWithoutChangingHistory(t *testing.T) {
	s, primary, backup := failoverGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	ctx := t.Context()
	scheme, err := s.Allocations().SaveScheme(ctx, 0, allocations.SchemeInput{Name: "Synthetic failover allowance", GroupID: 1, Enabled: true, Config: allocations.Config{Mode: "tokens", Period: "day", Members: []allocations.Share{{UserID: 1, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	createAdmissionKey(t, s, 42, 1, 1)
	if _, err := s.db.Exec("INSERT INTO allocation_keys(key_id,scheme_id) VALUES(42,?)", scheme.ID); err != nil {
		t.Fatal(err)
	}
	request := func() {
		t.Helper()
		x, err := s.Open(WithRequestIdentity(ctx, 42, "http"), 1, 1, []byte(`{"model":"synthetic-model","input":"Synthetic input"}`), http.Header{"Session_id": {failoverSession}}, Responses)
		if err != nil {
			t.Fatal(err)
		}
		defer x.Body.Close()
		if err := x.Events(func([]byte) error { return nil }); err != nil {
			t.Fatal(err)
		}
		x.Body.Close()
	}
	request()
	if _, err := s.accounts.SetEnabled(ctx, primary, false); err != nil {
		t.Fatal(err)
	}
	request()
	var entries, settled, accountsUsed int
	if err := s.db.QueryRow("SELECT count(*),sum(state='settled'),count(DISTINCT account_id) FROM allocation_entries").Scan(&entries, &settled, &accountsUsed); err != nil || entries != 2 || settled != 2 || accountsUsed != 2 {
		t.Fatal("allowance settlement duplicated or lost usage", entries, settled, accountsUsed, err)
	}
	page, err := s.Requests(ctx, RequestFilter{})
	if err != nil || len(page.Requests) != 2 {
		t.Fatal("request history lost", err)
	}
	seen := map[string]bool{}
	for _, r := range page.Requests {
		seen[r.AccountID] = true
		if r.Outcome != "success" {
			t.Fatal(r)
		}
	}
	if !seen[primary] || !seen[backup] {
		t.Fatal("historical account attribution changed")
	}
	if _, err := s.accounts.SetEnabled(ctx, primary, true); err != nil {
		t.Fatal(err)
	}
	if id, _, err := s.selectAccount(ctx, 1, 1, failoverSession, "", "synthetic-model", Responses); err != nil || id != backup {
		t.Fatal("recovery undid failover", id, err)
	}
}

func TestPreparationFailureRebalancesWithoutDuplicateAdmission(t *testing.T) {
	s, primary, backup := failoverGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	ctx := t.Context()
	if err := s.SetMemberLimits(ctx, 1, 100, 10); err != nil {
		t.Fatal(err)
	}
	scheme, err := s.Allocations().SaveScheme(ctx, 0, allocations.SchemeInput{Name: "Synthetic preparation allowance", GroupID: 1, Enabled: true, Config: allocations.Config{Mode: "tokens", Period: "day", Members: []allocations.Share{{UserID: 1, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	createAdmissionKey(t, s, 42, 1, 1)
	if _, err := s.db.Exec("INSERT INTO allocation_keys(key_id,scheme_id) VALUES(42,?)", scheme.ID); err != nil {
		t.Fatal(err)
	}
	entry, err := s.begin(WithRequestIdentity(ctx, 42, "http"), 1, 1, Responses)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.fail(context.Canceled)
	prepared := []string{}
	id, _, err := s.leaseAccount(ctx, entry, failoverSession, "", "synthetic-model", Responses, pinnedAffinity, "", func(id string) error {
		prepared = append(prepared, id)
		if id == primary {
			if _, err := s.accounts.SetEnabled(ctx, primary, false); err != nil {
				return err
			}
			return accounts.ErrDisabled
		}
		return nil
	})
	if err != nil || id != backup || len(prepared) != 2 {
		t.Fatal("preparation did not rebalance", id, prepared, err)
	}
	limits, err := s.MemberLimits(ctx, 1)
	if err != nil || limits.RequestsThisMinute != 1 || limits.InFlight != 1 {
		t.Fatal("preparation duplicated member admission", limits, err)
	}
	if s.health[primary].InFlight != 0 || s.health[backup].InFlight != 1 {
		t.Fatal("preparation leaked account lease")
	}
	var count int
	var captured string
	if err := s.db.QueryRow("SELECT count(*),account_id FROM allocation_entries").Scan(&count, &captured); err != nil || count != 1 || captured != backup {
		t.Fatal("preparation duplicated or misattributed allowance reservation", count, captured, err)
	}
}
