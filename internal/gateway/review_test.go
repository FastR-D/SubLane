package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/upstream"
)

func reviewGateway(t *testing.T) (*Service, string, string) {
	t.Helper()
	ctx := context.Background()
	s, ids := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return syntheticStream(), nil }))
	second, err := s.accounts.Authorize(ctx, "Synthetic second", accounts.Credential{AccountID: "synthetic-second", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ids["codex"], second.ID} {
		if err := s.accounts.SaveCatalog(ctx, id, 0, []string{"synthetic-model", "codex-auto-review"}, s.now().Unix(), upstream.CatalogSource("codex"), "codex-auto-review"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"], second.ID}}); err != nil {
		t.Fatal(err)
	}
	parent, _, err := s.selectAccount(ctx, 1, 1, "synthetic-parent", "", "synthetic-model", Responses)
	if err != nil {
		t.Fatal(err)
	}
	other := second.ID
	if other == parent {
		other = ids["codex"]
	}
	return s, parent, other
}

func reviewHeaders(session string) http.Header {
	h := make(http.Header)
	h.Set("Session_id", session)
	h.Set("X-OpenAI-Subagent", "guardian")
	h.Set("X-Codex-Parent-Thread-Id", "synthetic-parent")
	return h
}

func openReview(t *testing.T, s *Service, userID, groupID int64, headers http.Header) string {
	t.Helper()
	x, err := s.Open(context.Background(), userID, groupID, []byte(`{"model":"codex-auto-review","input":"Synthetic operation"}`), headers, Responses)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Body.Close()
	if err := x.Events(func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return x.entry.record.AccountID
}

func TestReviewPrefersParentWithoutChangingItsBinding(t *testing.T) {
	s, parent, _ := reviewGateway(t)
	if got := openReview(t, s, 1, 1, reviewHeaders("synthetic-review")); got != parent {
		t.Fatal("review lost its parent's account", got, parent)
	}
	var parentAccount, parentModel string
	if err := s.db.QueryRow("SELECT account_id,model FROM account_affinity WHERE model='synthetic-model'").Scan(&parentAccount, &parentModel); err != nil || parentAccount != parent || parentModel != "synthetic-model" {
		t.Fatal("review changed the parent binding", parentAccount, parentModel, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM account_affinity").Scan(&count); err != nil || count != 2 {
		t.Fatal("review did not retain its own session binding", count, err)
	}
	reopened := New(context.Background(), s.db, s.accounts, s.provider)
	defer reopened.Close()
	if got := openReview(t, reopened, 1, 1, reviewHeaders("synthetic-review-after-restart")); got != parent {
		t.Fatal("parent affinity did not survive restart", got, parent)
	}
}

func TestReviewFallsBackWhenParentIsUnavailable(t *testing.T) {
	for _, change := range []string{"disabled", "removed", "deleted", "reauthorize", "busy", "cooldown", "model cooldown", "quota", "unsupported", "expired catalog"} {
		t.Run(change, func(t *testing.T) {
			s, parent, other := reviewGateway(t)
			ctx := context.Background()
			switch change {
			case "disabled":
				if _, err := s.accounts.SetEnabled(ctx, parent, false); err != nil {
					t.Fatal(err)
				}
			case "removed":
				if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{other}}); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := s.accounts.Delete(ctx, parent); err != nil {
					t.Fatal(err)
				}
			case "reauthorize":
				if _, err := s.db.Exec("UPDATE accounts SET status='reauth_required' WHERE id=?", parent); err != nil {
					t.Fatal(err)
				}
			case "busy":
				s.health[parent].InFlight = s.health[parent].MaxConcurrency
				t.Cleanup(func() { s.health[parent].InFlight = 0 })
			case "cooldown":
				s.health[parent].CooldownUntil = s.now().Add(time.Minute).Unix()
			case "model cooldown":
				row, err := s.queries.GetAccountModelRuntime(ctx, db.GetAccountModelRuntimeParams{TargetID: parent, WorkspaceID: 1, Model: "codex-auto-review"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.queries.SaveAccountModelRuntime(ctx, db.SaveAccountModelRuntimeParams{TargetID: parent, WorkspaceID: 1, Model: "codex-auto-review", Lifecycle: row.Lifecycle, RuntimeRevision: row.RuntimeRevision, CooldownUntil: s.now().Add(time.Minute).Unix(), Failures: 1, Reason: "synthetic"}); err != nil {
					t.Fatal(err)
				}
			case "quota":
				saveExhaustedQuota(t, s, parent)
			case "unsupported":
				if err := s.accounts.SaveCatalog(ctx, parent, 0, []string{"synthetic-model"}, s.now().Unix(), upstream.CatalogSource("codex")); err != nil {
					t.Fatal(err)
				}
			case "expired catalog":
				if _, err := s.db.Exec("UPDATE accounts SET models_snapshot=json_set(models_snapshot,'$.updated_at',?) WHERE id=?", s.now().Add(-25*time.Hour).Unix(), parent); err != nil {
					t.Fatal(err)
				}
			}
			if got := openReview(t, s, 1, 1, reviewHeaders("synthetic-review")); got != other {
				t.Fatal("parent preference bypassed availability", got, other)
			}
			var original string
			if err := s.db.QueryRow("SELECT account_id FROM account_affinity WHERE model='synthetic-model'").Scan(&original); err != nil || original != parent {
				t.Fatal("fallback rewrote the main conversation", original, err)
			}
		})
	}
}

func TestReviewKeepsAnExistingChildBinding(t *testing.T) {
	s, parent, other := reviewGateway(t)
	bound, _, err := s.selectAccount(context.Background(), 1, 1, "synthetic-review", "", "codex-auto-review", Responses)
	if err != nil || bound != other {
		t.Fatal("synthetic child must start on the other account", bound, err)
	}
	if got := openReview(t, s, 1, 1, reviewHeaders("synthetic-review")); got != other {
		t.Fatal("parent hint moved an existing child", got, other)
	}
	if _, err := s.accounts.SetEnabled(context.Background(), other, false); err != nil {
		t.Fatal(err)
	}
	x, err := s.Open(context.Background(), 1, 1, []byte(`{"model":"codex-auto-review","input":"Synthetic operation"}`), reviewHeaders("synthetic-review"), Responses)
	if x != nil {
		x.Body.Close()
	}
	if err != nil || x == nil || x.entry.record.AccountID != parent {
		t.Fatal("unavailable child did not rebalance to eligible parent", parent, err)
	}
}

func TestReviewParentMetadata(t *testing.T) {
	metadata := func(parent, kind string) string {
		return fmt.Sprintf(`{"parent_thread_id":%q,"subagent_kind":%q}`, parent, kind)
	}
	for _, tc := range []struct {
		name, model, parent, subagent, header, body string
		kind                                        Kind
		duplicate                                   bool
		want                                        string
	}{
		{name: "dedicated headers", parent: "synthetic-parent", subagent: "guardian", want: "synthetic-parent"},
		{name: "header metadata", header: metadata("synthetic-parent", "review"), want: "synthetic-parent"},
		{name: "body metadata", body: metadata("synthetic-parent", "guardian"), want: "synthetic-parent"},
		{name: "matching sources", parent: "synthetic-parent", subagent: "guardian", header: metadata("synthetic-parent", "guardian"), body: metadata("synthetic-parent", "guardian"), want: "synthetic-parent"},
		{name: "thinking suffix", model: "codex-auto-review(high)", parent: "synthetic-parent", subagent: "guardian", want: "synthetic-parent"},
		{name: "case insensitive subagent", parent: "synthetic-parent", subagent: " Review ", want: "synthetic-parent"},
		{name: "ordinary model", model: "synthetic-model", parent: "synthetic-parent", subagent: "guardian"},
		{name: "other protocol", kind: Chat, parent: "synthetic-parent", subagent: "guardian"},
		{name: "compaction", kind: Compact, parent: "synthetic-parent", subagent: "guardian"},
		{name: "missing parent", subagent: "guardian"},
		{name: "missing review marker", parent: "synthetic-parent"},
		{name: "other subagent", parent: "synthetic-parent", subagent: "planner"},
		{name: "conflicting parents", parent: "synthetic-parent", subagent: "guardian", body: metadata("synthetic-other", "guardian")},
		{name: "conflicting subagents", parent: "synthetic-parent", subagent: "guardian", body: metadata("synthetic-parent", "review")},
		{name: "malformed header", parent: "synthetic-parent", subagent: "guardian", header: "{"},
		{name: "malformed body", parent: "synthetic-parent", subagent: "guardian", body: "{"},
		{name: "wrong parent type", header: `{"parent_thread_id":42,"subagent_kind":"guardian"}`},
		{name: "oversized parent", parent: strings.Repeat("x", 1025), subagent: "guardian"},
		{name: "oversized metadata", header: metadata("synthetic-parent", "guardian") + strings.Repeat(" ", 4096)},
		{name: "oversized client metadata", parent: "synthetic-parent", subagent: "guardian", body: metadata("synthetic-parent", "guardian") + strings.Repeat(" ", 8192)},
		{name: "duplicate headers", parent: "synthetic-parent", subagent: "guardian", duplicate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := make(http.Header)
			h.Set("X-Codex-Parent-Thread-Id", tc.parent)
			h.Set("X-OpenAI-Subagent", tc.subagent)
			h.Set("X-Codex-Turn-Metadata", tc.header)
			if tc.duplicate {
				h.Add("X-Codex-Parent-Thread-Id", "synthetic-other")
			}
			input := map[string]json.RawMessage{}
			if tc.body != "" {
				input["client_metadata"], _ = json.Marshal(map[string]string{"x-codex-turn-metadata": tc.body})
			}
			model := tc.model
			if model == "" {
				model = "codex-auto-review"
			}
			if got := reviewParentThread(input, h, model, tc.kind); got != tc.want {
				t.Fatalf("parent hint: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReviewParentScopeAndExpiry(t *testing.T) {
	for _, scope := range []string{"other user", "other group", "expired", "missing", "ambiguous", "legacy binding", "nonlegacy group", "sessionless", "opaque history", "explicit provider"} {
		t.Run(scope, func(t *testing.T) {
			s, parent, other := reviewGateway(t)
			ctx := context.Background()
			userID, groupID := int64(1), int64(1)
			h := reviewHeaders("synthetic-review")
			want := other
			switch scope {
			case "other user":
				userID = runtimeMember(t, s)
			case "other group", "nonlegacy group":
				pool, err := groups.New(s.db).Save(ctx, 0, groups.Input{Name: "Synthetic other", Enabled: true, AccountIDs: []string{parent, other}})
				if err != nil {
					t.Fatal(err)
				}
				groupID = pool.ID
				parentSession := ""
				if scope == "nonlegacy group" {
					parentSession = "synthetic-parent"
					want = parent
				}
				if id, _, err := s.selectAccount(ctx, userID, groupID, parentSession, "", "synthetic-model", Responses); err != nil || id != parent {
					t.Fatal("synthetic parent setup", id, err)
				}
				if scope == "nonlegacy group" {
					oldHash := sha256.Sum256([]byte(fmt.Sprintf("1:%d:synthetic-parent", groupID)))
					if _, err := s.db.Exec("UPDATE account_affinity SET session_hash=? WHERE group_id=? AND model='synthetic-model'", oldHash[:], groupID); err != nil {
						t.Fatal(err)
					}
				}
			case "expired":
				if _, err := s.db.Exec("UPDATE account_affinity SET expires_at=?", s.now().Unix()); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if _, err := s.db.Exec("DELETE FROM account_affinity"); err != nil {
					t.Fatal(err)
				}
			case "ambiguous":
				if _, err := s.db.Exec("UPDATE account_affinity SET provider='codex'"); err != nil {
					t.Fatal(err)
				}
				if _, err := s.db.Exec("INSERT INTO account_affinity(user_id,group_id,session_hash,provider,account_id,expires_at,model) SELECT user_id,group_id,session_hash,'claude',?,expires_at,model FROM account_affinity", other); err != nil {
					t.Fatal(err)
				}
			case "legacy binding":
				oldHash := sha256.Sum256([]byte("1:synthetic-parent"))
				if _, err := s.db.Exec("UPDATE account_affinity SET provider='codex',session_hash=?", oldHash[:]); err != nil {
					t.Fatal(err)
				}
				want = parent
			case "sessionless":
				h.Del("Session_id")
				want = parent
			case "opaque history":
				x, err := s.Open(ctx, userID, groupID, []byte(`{"model":"codex-auto-review","input":[{"type":"item_reference","id":"synthetic-item"}]}`), h, Responses)
				if err != nil {
					t.Fatal(err)
				}
				defer x.Body.Close()
				if x.entry.record.AccountID != other {
					t.Fatal("opaque continuation inherited a parent hint", x.entry.record.AccountID, other)
				}
				return
			case "explicit provider":
				if _, _, err := s.selectAccount(ctx, userID, groupID, "", "codex", "synthetic-model", Responses); err != nil {
					t.Fatal(err)
				}
				x, err := s.Open(ctx, userID, groupID, []byte(`{"model":"codex/codex-auto-review","input":"Synthetic operation"}`), h, Responses)
				if err != nil {
					t.Fatal(err)
				}
				defer x.Body.Close()
				if x.entry.record.AccountID != parent {
					t.Fatal("explicit provider lost its native parent binding", x.entry.record.AccountID, parent)
				}
				return
			}
			if got := openReview(t, s, userID, groupID, h); got != want {
				t.Fatal("parent hint escaped its scope", got, want)
			}
			if scope == "sessionless" {
				var count int
				if err := s.db.QueryRow("SELECT count(*) FROM account_affinity").Scan(&count); err != nil || count != 1 {
					t.Fatal("sessionless review created a binding", count, err)
				}
			}
		})
	}
}

func TestReviewParentRespectsResourceRoutingPolicy(t *testing.T) {
	s, parent, other := reviewGateway(t)
	ctx := context.Background()
	channel, err := s.accounts.ImportProvider(ctx, "openai", "Synthetic channel", []byte(`{"api_key":"synthetic-key","base_url":"https://relay.example.test/v1"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.accounts.SaveCatalog(ctx, channel.ID, 0, []string{"synthetic-model", "codex-auto-review"}, s.now().Unix(), upstream.CatalogSource("openai")); err != nil {
		t.Fatal(err)
	}
	pool := groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{parent, other, channel.ID}, Routing: &groups.Routing{Preference: "api_first", AllowAPIFallback: true}}
	if _, err := groups.New(s.db).Save(ctx, 1, pool); err != nil {
		t.Fatal(err)
	}
	if id, _, err := s.selectAccount(ctx, 1, 1, "", "", "codex-auto-review", Responses); err != nil || id != channel.ID {
		t.Fatal("synthetic API preference setup", id, err)
	}
	if got := openReview(t, s, 1, 1, reviewHeaders("synthetic-parent-preferred")); got != parent {
		t.Fatal("resource preference overrode an eligible parent", got, parent)
	}
	if _, err := s.db.Exec("UPDATE account_affinity SET account_id=? WHERE model='synthetic-model'", channel.ID); err != nil {
		t.Fatal(err)
	}
	pool.Routing = &groups.Routing{Preference: "subscription_first", AllowAPIFallback: false}
	if _, err := groups.New(s.db).Save(ctx, 1, pool); err != nil {
		t.Fatal(err)
	}
	if got := openReview(t, s, 1, 1, reviewHeaders("synthetic-api-denied")); got != parent && got != other {
		t.Fatal("parent hint bypassed the disabled API fallback", got)
	}
}

func TestReviewParentDoesNotBypassPolicyOrAllowance(t *testing.T) {
	s, parent, other := reviewGateway(t)
	ctx := context.Background()
	pools := groups.New(s.db)
	if _, err := pools.Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{parent, other}, ModelPolicy: &groups.ModelPolicy{Restricted: true, Models: []string{"synthetic-model"}}}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"model":"codex-auto-review","input":"Synthetic operation"}`)
	if _, err := s.Open(ctx, 1, 1, raw, reviewHeaders("synthetic-denied"), Responses); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatal("parent hint bypassed group allowlist", err)
	}
	if _, err := pools.Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{parent, other}, ModelPolicy: &groups.ModelPolicy{Models: []string{}}}); err != nil {
		t.Fatal(err)
	}
	scheme, err := s.Allocations().SaveScheme(ctx, 0, allocations.SchemeInput{Name: "Synthetic budget", GroupID: 1, Enabled: true, Config: allocations.Config{Mode: "tokens", Period: "day", Members: []allocations.Share{{UserID: 1, Limit: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := allocations.Begin(ctx, s.queries, allocations.Request{ID: "synthetic-used", SchemeID: scheme.ID, UserID: 1, GroupID: 1, AccountID: parent, Model: "codex-auto-review", StartedAt: s.now().Unix()}, s.now().Unix(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := allocations.Finish(ctx, s.queries, "synthetic-used", allocations.Completion{Input: 1, Known: true, Dispatched: true}, s.now().UnixMilli(), false); err != nil {
		t.Fatal(err)
	}
	result, err := s.db.Exec("INSERT INTO api_keys(user_id,group_id,name,prefix,token_hash,created_at) VALUES(1,1,'synthetic','sl_fake',randomblob(32),1)")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := result.LastInsertId()
	if _, err := s.db.Exec("INSERT INTO allocation_keys(key_id,scheme_id) VALUES(?,?)", key, scheme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(WithRequestIdentity(ctx, key, "http"), 1, 1, raw, reviewHeaders("synthetic-budget-denied"), Responses); !errors.Is(err, allocations.ErrQuota) {
		t.Fatal("parent hint bypassed the exhausted allowance", err)
	}
}
