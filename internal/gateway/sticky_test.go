package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/murongg/SubLane/internal/groups"
)

func stickyGateway(t *testing.T, sticky bool) (*Service, string, string) {
	t.Helper()
	s, parent, other := reviewGateway(t)
	setSticky(t, s, sticky)
	return s, parent, other
}

// setSticky toggles session stickiness on the fixture's group, keeping its resources.
func setSticky(t *testing.T, s *Service, sticky bool) {
	t.Helper()
	detail, err := groups.New(s.db).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := groups.New(s.db).Save(context.Background(), 1, groups.Input{Name: detail.Name, Enabled: true, AccountIDs: detail.AccountIDs, Routing: &groups.Routing{Preference: "protocol", SessionSticky: sticky}}); err != nil {
		t.Fatal(err)
	}
}

func openSticky(t *testing.T, s *Service, headers http.Header, body string) string {
	t.Helper()
	x, err := s.Open(context.Background(), 1, 1, []byte(body), headers, Responses)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Body.Close()
	if err := x.Events(func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return x.entry.record.AccountID
}

const stickyOpening = `{"model":"synthetic-model","instructions":"Synthetic agent","input":[{"role":"user","content":"Synthetic opening"}]}`

func TestSessionlessRequestsRotateUnlessStickinessIsEnabled(t *testing.T) {
	s, _, _ := stickyGateway(t, false)
	seen := map[string]bool{}
	for range 4 {
		seen[openSticky(t, s, http.Header{}, stickyOpening)] = true
	}
	if len(seen) != 2 {
		t.Fatalf("default routing no longer rotates sessionless requests: %v", seen)
	}

	s, _, _ = stickyGateway(t, true)
	first := openSticky(t, s, http.Header{}, stickyOpening)
	// A later turn keeps the same opening and therefore the same upstream resource.
	later := `{"model":"synthetic-model","instructions":"Synthetic agent","input":[{"role":"user","content":"Synthetic opening"},{"role":"assistant","content":"Synthetic reply"},{"role":"user","content":"Synthetic follow-up"}]}`
	for range 4 {
		if got := openSticky(t, s, http.Header{}, later); got != first {
			t.Fatalf("sticky conversation moved from %s to %s", first, got)
		}
	}
	var expires, now int64
	if err := s.db.QueryRow("SELECT expires_at, unixepoch() FROM account_affinity WHERE account_id=? AND model='synthetic-model' ORDER BY expires_at LIMIT 1", first).Scan(&expires, &now); err != nil {
		t.Fatal(err)
	}
	if expires-now > derivedAffinitySeconds+60 {
		t.Fatalf("derived binding uses the durable lifetime: %d", expires-now)
	}
}

func TestStickySubagentsPreferTheirParent(t *testing.T) {
	child := func(session string) http.Header {
		h := http.Header{}
		h.Set("Session_id", session)
		h.Set("X-Codex-Parent-Thread-Id", "synthetic-parent")
		return h
	}
	s, parent, _ := stickyGateway(t, false)
	seen := map[string]bool{}
	for _, session := range []string{"synthetic-child-a", "synthetic-child-b", "synthetic-child-c", "synthetic-child-d"} {
		seen[openSticky(t, s, child(session), stickyOpening)] = true
	}
	if len(seen) != 2 {
		t.Fatalf("parent metadata changed default routing: %v", seen)
	}

	s, parent, _ = stickyGateway(t, true)
	for _, session := range []string{"synthetic-child-a", "synthetic-child-b", "synthetic-child-c"} {
		if got := openSticky(t, s, child(session), stickyOpening); got != parent {
			t.Fatalf("subagent %s used %s instead of parent %s", session, got, parent)
		}
	}
}

func TestDerivedStickinessFallsBackWhenTheResourceIsUnavailable(t *testing.T) {
	s, _, _ := stickyGateway(t, true)
	first := openSticky(t, s, http.Header{}, stickyOpening)
	if _, err := s.accounts.SetEnabled(context.Background(), first, false); err != nil {
		t.Fatal(err)
	}
	next := openSticky(t, s, http.Header{}, stickyOpening)
	if next == "" || next == first {
		t.Fatalf("derived binding did not fail over: %q", next)
	}
	// The preference follows the replacement instead of returning to the disabled resource.
	if got := openSticky(t, s, http.Header{}, stickyOpening); got != next {
		t.Fatalf("replacement preference was not retained: %s", got)
	}
}

func TestStickyHintsReadClientConversationMetadata(t *testing.T) {
	decode := func(raw string) map[string]json.RawMessage {
		var input map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
		return input
	}
	claude := decode(`{"metadata":{"user_id":"{\"device_id\":\"synthetic\",\"session_id\":\"0f8c8a4e-synthetic\",\"parent_session_id\":\"9a1b-parent\"}"},"messages":[]}`)
	hints := requestStickyHints(http.Header{}, claude, Messages, `{"device_id":"synthetic"}`, true)
	if hints.Session != "claude:0f8c8a4e-synthetic" || !hints.Replace || hints.Derived || hints.Parent != "claude:9a1b-parent" {
		t.Fatalf("claude JSON metadata: %+v", hints)
	}
	legacy := decode(`{"metadata":{"user_id":"user_synthetic_account__session_7d3e2c1a-0000-4000-8000-000000000000"}}`)
	if hints := requestStickyHints(http.Header{}, legacy, Messages, "raw", true); hints.Session != "claude:7d3e2c1a-0000-4000-8000-000000000000" {
		t.Fatalf("claude legacy metadata: %+v", hints)
	}
	headers := http.Header{}
	headers.Set("X-Codex-Turn-Metadata", `{"session_id":"synthetic-turn","parent_thread_id":"synthetic-parent"}`)
	if hints := requestStickyHints(headers, decode(`{"input":"x"}`), Responses, "", false); hints.Session != "synthetic-turn" || !hints.Derived || hints.Parent != "synthetic-parent" {
		t.Fatalf("codex turn metadata: %+v", hints)
	}
	// Explicit gateway sessions are never replaced, and a session is never its own parent.
	headers.Set("X-Codex-Turn-Metadata", `{"parent_thread_id":"synthetic-explicit"}`)
	if hints := requestStickyHints(headers, decode(`{"input":"x"}`), Responses, "synthetic-explicit", false); hints.Session != "" || hints.Parent != "" {
		t.Fatalf("explicit session: %+v", hints)
	}
	duplicate := http.Header{"X-Session-Id": {"a", "b"}}
	if hints := requestStickyHints(duplicate, decode(`{"input":[]}`), Responses, "", false); hints.Session != "" {
		t.Fatalf("ambiguous header was trusted: %+v", hints)
	}
	if messageSession(decode(`{"messages":[{"role":"user","content":"same"}]}`), Chat) != messageSession(decode(`{"messages":[{"role":"user","content":[{"type":"text","text":"same"}]},{"role":"assistant","content":"later"}]}`), Chat) {
		t.Fatal("message fallback changed between turns")
	}
}

func TestDerivedPreferencesNeverRewriteExplicitBindings(t *testing.T) {
	s, _, _ := stickyGateway(t, true)
	explicit := http.Header{}
	explicit.Set("Session_id", "synthetic-shared-id")
	original := openSticky(t, s, explicit, stickyOpening)
	derived := http.Header{}
	derived.Set("X-Session-Id", "synthetic-shared-id")
	if _, err := s.accounts.SetEnabled(context.Background(), original, false); err != nil {
		t.Fatal(err)
	}
	if fallback := openSticky(t, s, derived, stickyOpening); fallback == original {
		t.Fatal("derived request used the disabled account")
	}
	if _, err := s.accounts.SetEnabled(context.Background(), original, true); err != nil {
		t.Fatal(err)
	}
	// The explicit continuation keeps its own binding regardless of the derived fallback.
	if got := openSticky(t, s, explicit, stickyOpening); got != original {
		t.Fatalf("explicit session moved from %s to %s", original, got)
	}
}

func TestDerivedPreferencesNeverBlockExplicitSessions(t *testing.T) {
	s, _, _ := stickyGateway(t, true)
	var existing int
	if err := s.db.QueryRow("SELECT count(*) FROM account_affinity").Scan(&existing); err != nil {
		t.Fatal(err)
	}
	// Leave exactly one free slot in the shared table.
	if _, err := s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < ?)
		INSERT INTO account_affinity(user_id,group_id,session_hash,provider,account_id,expires_at,model,rebound,derived)
		SELECT 1,1,randomblob(32),'auto','synthetic-filler',unixepoch()+3600,'',0,0 FROM n`, affinityLimit-1-existing); err != nil {
		t.Fatal(err)
	}
	openSticky(t, s, http.Header{}, stickyOpening)
	var derived int
	if err := s.db.QueryRow("SELECT count(*) FROM account_affinity WHERE derived=1").Scan(&derived); err != nil || derived != 1 {
		t.Fatal("derived preference did not take the free slot", derived, err)
	}
	explicit := http.Header{}
	explicit.Set("Session_id", "synthetic-new-explicit")
	openSticky(t, s, explicit, stickyOpening)
	var total int
	if err := s.db.QueryRow("SELECT count(*), sum(derived) FROM account_affinity").Scan(&total, &derived); err != nil || total != affinityLimit || derived != 0 {
		t.Fatal("explicit session did not displace the preference", total, derived, err)
	}
	// With the table full of explicit bindings, preferences are skipped rather than rejected.
	if got := openSticky(t, s, http.Header{}, `{"model":"synthetic-model","input":[{"role":"user","content":"Another synthetic opening"}]}`); got == "" {
		t.Fatal("derived request was rejected")
	}
}

func TestStickinessHonorsBindingsMadeBeforeOptIn(t *testing.T) {
	s, _, _ := stickyGateway(t, false)
	ctx := context.Background()
	raw := `{"device_id":"synthetic","session_id":"synthetic-claude-session"}`
	original, _, err := s.selectAccount(ctx, 1, 1, raw, "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	setSticky(t, s, true)
	if _, err := s.accounts.SetEnabled(ctx, original, false); err != nil {
		t.Fatal(err)
	}
	hints := withStickyHints(ctx, stickyHints{Session: "claude:synthetic-claude-session", Replace: true})
	// The raw metadata binding stays authoritative; provider state must not move to a fallback.
	got, _, err := s.selectAllocationAccount(hints, 1, 1, raw, "", "synthetic-model", Responses, 0, pinnedAffinity, "")
	if err == nil || got != original {
		t.Fatalf("bound conversation moved after opt-in: %q %v", got, err)
	}
	if _, err := s.accounts.SetEnabled(ctx, original, true); err != nil {
		t.Fatal(err)
	}
	// Unbound conversations adopt the normalized key, shared by the whole agent tree.
	fresh := withStickyHints(ctx, stickyHints{Session: "claude:synthetic-new-session", Replace: true})
	first, _, err := s.selectAllocationAccount(fresh, 1, 1, `{"session_id":"synthetic-new-session","agent_id":"a"}`, "", "synthetic-model", Responses, 0, pinnedAffinity, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"b", "c", "d"} {
		got, _, err := s.selectAllocationAccount(fresh, 1, 1, `{"session_id":"synthetic-new-session","agent_id":"`+agent+`"}`, "", "synthetic-model", Responses, 0, pinnedAffinity, "")
		if err != nil || got != first {
			t.Fatal("agent tree did not share the normalized binding", agent, got, err)
		}
	}
}

func TestNormalizedBindingsSurviveOptOut(t *testing.T) {
	s, _, _ := stickyGateway(t, true)
	ctx := context.Background()
	raw := `{"device_id":"synthetic","session_id":"synthetic-opt-out"}`
	hints := withStickyHints(ctx, stickyHints{Session: "claude:synthetic-opt-out", Replace: true})
	original, _, err := s.selectAllocationAccount(hints, 1, 1, raw, "", "synthetic-model", Responses, 0, pinnedAffinity, "")
	if err != nil {
		t.Fatal(err)
	}
	setSticky(t, s, false)
	if _, err := s.accounts.SetEnabled(ctx, original, false); err != nil {
		t.Fatal(err)
	}
	// Turning the setting off must not turn a bound continuation into a new session.
	got, _, err := s.selectAllocationAccount(hints, 1, 1, raw, "", "synthetic-model", Responses, 0, pinnedAffinity, "")
	if err == nil || got != original {
		t.Fatalf("normalized binding was dropped after opt-out: %q %v", got, err)
	}
	if _, err := s.accounts.SetEnabled(ctx, original, true); err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.selectAllocationAccount(hints, 1, 1, raw, "", "synthetic-model", Responses, 0, pinnedAffinity, ""); err != nil || got != original {
		t.Fatal("continuation left its normalized binding", got, err)
	}
	// With the setting off, unbound conversations keep using their raw key and create no normalized binding.
	fresh := withStickyHints(ctx, stickyHints{Session: "claude:synthetic-unbound", Replace: true})
	if _, _, err := s.selectAllocationAccount(fresh, 1, 1, `{"session_id":"synthetic-unbound"}`, "", "synthetic-model", Responses, 0, pinnedAffinity, ""); err != nil {
		t.Fatal(err)
	}
	normalized := sessionDigest(1, 1, "claude:synthetic-unbound")
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM account_affinity WHERE session_hash=?", normalized[:]).Scan(&count); err != nil || count != 0 {
		t.Fatal("opt-out created a normalized binding", count, err)
	}
}
