package allocations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/pricing"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/vault"
)

func fixture(t *testing.T) (*Service, *sql.DB, int64, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	conn, err := storage.Open(ctx, filepath.Join(dir, "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	a, err := auth.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	u, err := a.CreateMember(ctx, "synthetic-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	account, err := accounts.New(conn, v).Authorize(ctx, "Synthetic", accounts.Credential{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", AccountID: "synthetic-subject", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := groups.New(conn).Save(ctx, 0, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	pool, err := groups.New(conn).Save(ctx, 0, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{account.ID}})
	if err != nil || pool.ID != 2 {
		t.Fatal(pool, err)
	}
	if _, err = groups.New(conn).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := groups.New(conn).SetMemberGroups(ctx, u.ID, []int64{2}); err != nil {
		t.Fatal(err)
	}
	return New(conn), conn, u.ID, account.ID
}

func grantPoolMembers(ctx context.Context, conn *sql.DB, poolID int64, members ...int64) error {
	for _, member := range members {
		if _, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO memberships(tenant_id,user_id,role,created_at)
			VALUES(1,?,'member',1)`, member); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "INSERT OR IGNORE INTO group_members(group_id,user_id) VALUES(?,?)", poolID, member); err != nil {
			return err
		}
	}
	return nil
}

func TestWindowFollowsConfiguredZoneAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start, end := Window(time.Date(2030, 3, 10, 16, 0, 0, 0, time.UTC), Config{Period: "day"}, location)
	if start != time.Date(2030, 3, 10, 5, 0, 0, 0, time.UTC).Unix() || end != time.Date(2030, 3, 11, 4, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(start, end)
	}
	start, end = Window(time.Date(2030, 3, 15, 0, 0, 0, 0, time.UTC), Config{Period: "month"}, location)
	if start != time.Date(2030, 3, 1, 5, 0, 0, 0, time.UTC).Unix() || end != time.Date(2030, 4, 1, 4, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(start, end)
	}
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	start, end = Window(time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC), Config{Period: "day"}, santiago)
	if start != time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC).Unix() || end != time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC).Unix() {
		t.Fatal("midnight DST transition", start, end)
	}
}

func TestCustomResetSchedulesUseLocalWallTime(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	daily := Config{Period: "day", ResetTime: "09:30"}
	start, end := Window(time.Date(2030, 4, 1, 0, 0, 0, 0, time.UTC), daily, shanghai)
	if start != time.Date(2030, 3, 31, 1, 30, 0, 0, time.UTC).Unix() || end != time.Date(2030, 4, 1, 1, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("before daily reset", start, end)
	}
	start, end = Window(time.Date(2030, 4, 1, 2, 0, 0, 0, time.UTC), daily, shanghai)
	if start != time.Date(2030, 4, 1, 1, 30, 0, 0, time.UTC).Unix() || end != time.Date(2030, 4, 2, 1, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("after daily reset", start, end)
	}
	monthly := Config{Period: "month", ResetDay: 31, ResetTime: "09:30"}
	start, end = Window(time.Date(2030, 2, 28, 0, 0, 0, 0, time.UTC), monthly, shanghai)
	if start != time.Date(2030, 1, 31, 1, 30, 0, 0, time.UTC).Unix() || end != time.Date(2030, 2, 28, 1, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("short month before reset", start, end)
	}
	start, end = Window(time.Date(2030, 2, 28, 2, 0, 0, 0, time.UTC), monthly, shanghai)
	if start != time.Date(2030, 2, 28, 1, 30, 0, 0, time.UTC).Unix() || end != time.Date(2030, 3, 31, 1, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("short month after reset", start, end)
	}
}

func TestCustomResetTimeHandlesSkippedAndRepeatedHours(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Period: "day", ResetTime: "02:30"}
	start, end := Window(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC), c, newYork)
	if start != time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC).Unix() || end != time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("skipped local time", start, end)
	}
	c.ResetTime = "01:30"
	start, _ = Window(time.Date(2026, 11, 1, 6, 15, 0, 0, time.UTC), c, newYork)
	if start != time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).Unix() {
		t.Fatal("repeated local time", start)
	}
}

func TestResetScheduleValidation(t *testing.T) {
	base := Config{Mode: "tokens", Period: "month", Members: []Share{{UserID: 1, Limit: 1}}}
	for _, change := range []Config{
		{Mode: "tokens", Period: "month", ResetTime: "24:00", Members: base.Members},
		{Mode: "tokens", Period: "month", ResetDay: 32, Members: base.Members},
		{Mode: "tokens", Period: "day", ResetDay: 2, Members: base.Members},
	} {
		if err := normalize(&change); !errors.Is(err, ErrInput) {
			t.Fatal(change, err)
		}
	}
}

func TestStoredRevisionRejectsInvalidResetClock(t *testing.T) {
	_, err := decodeRevision(1, 1, `{"mode":"tokens","period":"day","reset_time":"broken","members":[],"rates":[]}`)
	if !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
}

func TestNewRevisionDoesNotCountPreviousCycleAfterResetTimeChange(t *testing.T) {
	effective := time.Date(2030, 4, 2, 0, 0, 0, 0, time.UTC).Unix()
	rev := Revision{EffectiveAt: effective, AccountingAnchorAt: effective, Config: Config{Mode: "tokens", Period: "day", ResetTime: "09:00"}}
	start, end := effectiveWindow(rev, effective+3600, time.UTC)
	if start != effective || end != time.Date(2030, 4, 2, 9, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(start, end)
	}
}

func TestScheduledSchemeUsesConfiguredMonthlyReset(t *testing.T) {
	s, conn, user, _ := fixture(t)
	ctx := context.Background()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	s.SetLocation(func() *time.Location { return location })
	now := time.Date(2030, 4, 15, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic schedule", GroupID: 2, Enabled: true, StartNext: true, Config: Config{Mode: "tokens", Period: "month", ResetDay: 15, ResetTime: "09:30", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2030, 4, 15, 1, 30, 0, 0, time.UTC).Unix()
	if scheme.EffectiveAt != want {
		t.Fatal("scheduled activation", scheme.EffectiveAt, want)
	}
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil || detail.Available || detail.Config.ResetDay != 15 || detail.Config.ResetTime != "09:30" {
		t.Fatal(detail, err)
	}
	reloaded := New(conn)
	reloaded.SetLocation(func() *time.Location { return location })
	now = time.Unix(want+60, 0)
	reloaded.now = func() time.Time { return now }
	detail, err = reloaded.Detail(ctx, scheme.ID, user)
	if err != nil || !detail.Available || detail.Balances[0].ResetAt != time.Date(2030, 5, 15, 1, 30, 0, 0, time.UTC).Unix() {
		t.Fatal(detail, err)
	}
}

func TestChangingZoneKeepsCurrentPeriodUsage(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	started := time.Date(2030, 3, 31, 17, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return time.Unix(started-60, 0) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic budget", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 10}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	r := Request{ID: "synthetic-zone-switch", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: started}
	if err := Begin(ctx, q, r, started, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, r.ID, Completion{Input: 10, Known: true, Dispatched: true}, started*1000, false); err != nil {
		t.Fatal(err)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	s.SetLocation(func() *time.Location { return shanghai })
	s.now = func() time.Time { return time.Unix(started+3600, 0) }
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", started+3600, shanghai); !errors.Is(err, ErrQuota) {
		t.Fatal("usage escaped when zone changed", err)
	}
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Balances[0].Used != 10 || detail.Balances[0].ResetAt != time.Date(2030, 4, 1, 16, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(detail.Balances[0])
	}
}

func TestAllocationSchemesStayInWorkspace(t *testing.T) {
	first, conn, member, _ := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`INSERT INTO users(id,username,role,password_hash,enabled,created_at)
		VALUES(3,'synthetic-other-owner','member','synthetic-hash',1,1);
		INSERT INTO tenants(id,name,owner_user_id,created_at) VALUES(2,'Other workspace',3,1);
		INSERT INTO memberships(tenant_id,user_id,role,created_at) VALUES(2,3,'owner',1);
		INSERT INTO accounts(id,tenant_id,provider,name,account_id,status,credential,created_at,updated_at)
		VALUES('other-account',2,'codex','Other account','other-subject','ready',x'01',1,1)`); err != nil {
		t.Fatal(err)
	}
	otherPool, err := groups.NewForTenant(conn, 2).Save(ctx, 0, groups.Input{Name: "Other pool", Enabled: true, AccountIDs: []string{"other-account"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := groups.NewForTenant(conn, 2).SetMemberGroups(ctx, 3, []int64{otherPool.ID}); err != nil {
		t.Fatal(err)
	}
	second := NewForTenant(conn, 2)
	firstScheme, err := first.SaveScheme(ctx, 0, SchemeInput{Name: "First allowance", GroupID: 2, Enabled: true,
		Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: member, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	secondScheme, err := second.SaveScheme(ctx, 0, SchemeInput{Name: "Second allowance", GroupID: otherPool.ID, Enabled: true,
		Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: 3, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Detail(ctx, firstScheme.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign workspace allocation opened: %v", err)
	}
	for _, check := range []struct {
		service *Service
		own     int64
		other   int64
	}{{first, firstScheme.ID, secondScheme.ID}, {second, secondScheme.ID, firstScheme.ID}} {
		all, err := check.service.Schemes(ctx)
		if err != nil || len(all) != 1 || all[0].ID != check.own {
			t.Fatalf("allocation list leaked across workspaces: %+v, %v", all, err)
		}
		if _, err := check.service.Detail(ctx, check.other, 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign workspace allocation was readable: %v", err)
		}
	}
}

func TestPoolAllocationUsesDirectMemberGrant(t *testing.T) {
	s, conn, user, _ := fixture(t)
	ctx := context.Background()
	if err := groups.New(conn).SetMemberGroups(ctx, user, []int64{2}); err != nil {
		t.Fatal(err)
	}
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic direct allocation", GroupID: 2, Enabled: true,
		Config: Config{Mode: "tokens", Period: "month", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatalf("pool allocation still requires a personnel team: %v", err)
	}
	if _, err := Authorize(ctx, db.New(conn), scheme.ID, user, 2, time.Now().Unix()); err != nil {
		t.Fatalf("directly granted member could not use the allocation: %v", err)
	}
}

func TestRatioTokenBudgetChargesReportedTokensWithoutQuotaSnapshot(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic shared token budget", GroupID: 2, Enabled: true,
		Config: Config{Mode: "ratio", RatioUnit: "tokens", Period: "month", Total: 1_000_000, Members: []Share{{UserID: user, Limit: 2500}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	request := Request{ID: "synthetic-ratio-token-a", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, request, now, time.UTC); err != nil {
		t.Fatal("share budget required an upstream snapshot", err)
	}
	if err := Finish(ctx, q, request.ID, Completion{Input: 200_000, Output: 10_000, Cached: 150_000, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil || len(detail.Balances) != 1 || detail.Balances[0].Mode != "tokens" || detail.Balances[0].Limit != 250_000 || detail.Balances[0].Used != 210_000 || len(detail.Pending) != 0 {
		t.Fatal("share budget did not charge reported tokens exactly once", detail, err)
	}
	request.ID = "synthetic-ratio-token-b"
	if err := Begin(ctx, q, request, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, request.ID, Completion{Input: 40_000, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("member exceeded percentage of total token budget", err)
	}
	now = time.Date(2030, 5, 1, 0, 0, 0, 0, time.UTC).Unix()
	request.StartedAt = now
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("new month did not reset token share", err)
	}
	request.ID = "synthetic-unknown-tokens"
	if err := Begin(ctx, q, request, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, request.ID, Completion{Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("one unknown report interrupted the member", err)
	}
	request.ID = "synthetic-unknown-tokens-two"
	if err := Begin(ctx, q, request, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, request.ID, Completion{Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("two unknown requests exhausted a still-usable allowance", err)
	}
	now = time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC).Unix()
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("previous cycle pending entries froze the new cycle", err)
	}
}

func TestRatioTokenBudgetRequiresUsableTotalAndShares(t *testing.T) {
	s, _, user, _ := fixture(t)
	ctx := context.Background()
	in := SchemeInput{Name: "Synthetic shared token budget", GroupID: 2, Enabled: true,
		Config: Config{Mode: "ratio", RatioUnit: "tokens", Period: "month", Members: []Share{{UserID: user, Limit: 2500}}}}
	if _, err := s.SaveScheme(ctx, 0, in); !errors.Is(err, ErrInput) {
		t.Fatal("missing total accepted", err)
	}
	in.Config.Total = 10
	in.Config.Members[0].Limit = 1
	if _, err := s.SaveScheme(ctx, 0, in); !errors.Is(err, ErrInput) {
		t.Fatal("share rounding to zero accepted", err)
	}
	in.Config.Total = 1_000_000
	in.Config.Members[0].Limit = 10_001
	if _, err := s.SaveScheme(ctx, 0, in); !errors.Is(err, ErrInput) {
		t.Fatal("percentage over 100 accepted", err)
	}
}

func TestRatioAmountBudgetUsesSavedModelPrices(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic amount shares", GroupID: 2, Enabled: true,
		Config: Config{Mode: "ratio", RatioUnit: "amount", Period: "month", Total: 2_000_000,
			Members: []Share{{UserID: user, Limit: 5000}},
			Rates:   []Rate{{Model: "synthetic", Input: 2_000_000, Cached: 200_000, Output: 4_000_000}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	request := Request{ID: "synthetic-ratio-amount", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, request, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, request.ID, Completion{Input: 500_000, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil || len(detail.Balances) != 1 || detail.Balances[0].Mode != "amount" || detail.Balances[0].Limit != 1_000_000 || detail.Balances[0].Used != 1_000_000 {
		t.Fatal("amount share was not charged at the saved model price", detail, err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("member exceeded monetary percentage", err)
	}
}

func TestWorkspaceOwnerCanReceiveAllowanceWithoutPoolGrant(t *testing.T) {
	service, conn, _, _ := fixture(t)
	ctx := context.Background()
	var grants int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM group_members WHERE group_id=2 AND user_id=1").Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("owner unexpectedly has a direct grant: %d, %v", grants, err)
	}
	scheme, err := service.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic owner allowance", GroupID: 2, Enabled: true,
		Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: 1, Limit: 100}}}})
	if err != nil || len(scheme.Config.Members) != 1 || scheme.Config.Members[0].UserID != 1 {
		t.Fatalf("owner allowance: %+v, %v", scheme, err)
	}
	if _, err := Authorize(ctx, db.New(conn), scheme.ID, 1, scheme.GroupID, scheme.EffectiveAt); err != nil {
		t.Fatalf("owner could not use the assigned allowance: %v", err)
	}
	for _, userID := range []int64{0, 1} {
		detail, err := service.Detail(ctx, scheme.ID, userID)
		if err != nil || !detail.Available || len(detail.Balances) != 1 {
			t.Fatalf("owner allowance detail for user %d: %+v, %v", userID, detail, err)
		}
		if balance := detail.Balances[0]; balance.UserID != 1 || balance.Username != "synthetic-admin" || balance.Limit != 100 {
			t.Fatalf("incorrect owner balance: %+v", balance)
		}
	}
	own, err := service.Own(ctx, 1)
	if err != nil || len(own) != 1 || own[0].ID != scheme.ID {
		t.Fatalf("owner allowance discovery: %+v, %v", own, err)
	}
}

func TestPlatformOwnerCanReceiveAllowanceAsMemberOfAnotherWorkspace(t *testing.T) {
	_, conn, ownerID, _ := fixture(t)
	ctx := context.Background()
	workspace, err := tenants.New(conn).Create(ctx, ownerID, "Second workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := tenants.New(conn).AddMember(ctx, ownerID, workspace.ID, 1, tenants.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO accounts(id,tenant_id,provider,name,account_id,status,credential,created_at,updated_at)
		VALUES('second-account',?,'codex','Synthetic account','synthetic-subject','ready',x'01',1,1)`, workspace.ID); err != nil {
		t.Fatal(err)
	}
	pools := groups.NewForTenant(conn, workspace.ID)
	pool, err := pools.Save(ctx, 0, groups.Input{Name: "Second pool", Enabled: true, AccountIDs: []string{"second-account"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := pools.SetMemberGroups(ctx, 1, []int64{pool.ID}); err != nil {
		t.Fatal(err)
	}
	service := NewForTenant(conn, workspace.ID)
	scheme, err := service.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic allowance", GroupID: pool.ID, Enabled: true,
		Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: 1, Limit: 100}}}})
	if err != nil || len(scheme.Config.Members) != 1 || scheme.Config.Members[0].UserID != 1 {
		t.Fatalf("workspace member 1 could not receive allowance: %+v, %v", scheme, err)
	}
	own, err := service.Own(ctx, 1)
	if err != nil || len(own) != 1 || !own[0].Available || len(own[0].Balances) != 1 {
		t.Fatalf("platform owner's workspace allowance: %+v, %v", own, err)
	}
	if balance := own[0].Balances[0]; balance.UserID != 1 || balance.Username != "synthetic-admin" || balance.Limit != 100 {
		t.Fatalf("incorrect platform owner balance: %+v", balance)
	}
}

func TestTeamsSchemesAndExclusiveCapacity(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	input := SchemeInput{Name: "Synthetic scheme", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "month", Members: []Share{{UserID: user, Limit: 1500000}}, Rates: []Rate{}}}
	scheme, err := s.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	if scheme.Config.Mode != "tokens" || scheme.Config.Members[0].Limit != 1500000 {
		t.Fatal(scheme)
	}
	if _, err = s.SaveScheme(ctx, 0, input); !errors.Is(err, ErrPoolConflict) {
		t.Fatal("duplicated pool budget", err)
	}
	if _, err = groups.New(conn).Save(ctx, 0, groups.Input{Name: "Conflicting", Enabled: true, AccountIDs: []string{account}}); err == nil {
		t.Fatal("shared account escaped exclusive scheme")
	}
	input.Config.Mode = "ratio"
	input.Config.RatioUnit = "tokens"
	input.Config.Total = 1_000_000
	input.Config.Period = "month"
	input.Config.Members[0].Limit = 10001
	if _, err = s.SaveScheme(ctx, scheme.ID, conditioned(t, s, scheme.ID, input)); !errors.Is(err, ErrInput) {
		t.Fatal("overallocated ratio", err)
	}
	input.Config.Mode = "amount"
	input.Config.RatioUnit = ""
	input.Config.Total = 0
	input.Config.Period = "month"
	input.Config.Members[0].Limit = 1000000
	if _, err = s.SaveScheme(ctx, scheme.ID, conditioned(t, s, scheme.ID, input)); !errors.Is(err, ErrUnpriced) {
		t.Fatal("unpriced amount scheme", err)
	}
}

func TestSchemeCopiesAutomaticPriceIntoRevision(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["gpt-5.1-codex"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"gpt-5.1-codex": {Input: 1250000, Cached: 125000, Output: 10000000},
	}))
	scheme, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Codex shared plan", GroupID: 2, Enabled: true, Config: Config{Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	rate := scheme.Config.Rates[0]
	if rate.Input != 1250000 || rate.Cached != 125000 || rate.Output != 10000000 {
		t.Fatalf("automatic price not snapshotted: %+v", rate)
	}
}

func TestAutomaticPricesFollowGroupAllowlistAndIgnoreSubmittedRates(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced","synthetic-unpriced"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE account_groups SET restricted_models=1 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO group_models(group_id,model) VALUES(2,'synthetic-priced')`); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Cached: 100_000, Output: 2_000_000},
	}))
	scheme, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic catalog pricing", GroupID: 2, Enabled: true, Config: Config{
		Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 3600, Limit: 100}},
		Members: []Share{{UserID: user}}, Rates: []Rate{{Model: "synthetic-priced", Input: 999_999_999, Output: 999_999_999}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scheme.Config.Rates) != 1 || scheme.Config.Rates[0].Model != "synthetic-priced" || scheme.Config.Rates[0].Input != 1_000_000 || scheme.Config.Rates[0].Output != 2_000_000 {
		t.Fatal("group catalog prices were not snapshotted", scheme.Config.Rates)
	}
}

func TestAutomaticPricesSavePricedModelsAndWarnAboutMissingOnes(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced","synthetic-new"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	}))
	scheme, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic partial pricing", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scheme.Config.Rates) != 1 || scheme.Config.Rates[0].Model != "synthetic-priced" {
		t.Fatal("priced model was not saved", scheme.Config.Rates)
	}
	if len(scheme.PriceCoverage.UncoveredModels) != 1 || scheme.PriceCoverage.UncoveredModels[0] != "synthetic-new" {
		t.Fatal("uncovered model was not returned", scheme.PriceCoverage)
	}
	if _, err := rateFor(scheme.Revision, "synthetic-new"); !errors.Is(err, ErrUnpriced) {
		t.Fatal("unpriced request should remain blocked", err)
	}
}

func TestPriceCoverageIgnoresHiddenModels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config Config
		limit  int64
	}{
		{"amount", Config{Mode: "amount", Period: "day"}, 1_000_000},
		{"ratio", Config{Mode: "ratio", RatioUnit: "amount", Total: 1_000_000, Period: "day"}, 10000},
		{"windows", Config{Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 3600, Limit: 1_000_000}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, conn, user, account := fixture(t)
			ctx := context.Background()
			if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
				`{"models":["synthetic-priced","synthetic-stale","synthetic-new","synthetic-hidden-priced","synthetic-hidden-new"],"hidden_models":["synthetic-hidden-priced","synthetic-hidden-new"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
				t.Fatal(err)
			}
			price := pricing.Price{Input: 1_000_000, Output: 2_000_000}
			manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
				"synthetic-priced": price, "synthetic-stale": price, "synthetic-hidden-priced": price,
			}))
			now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
			manager.now = func() time.Time { return unix(now) }
			tc.config.Members = []Share{{UserID: user, Limit: tc.limit}}
			input := SchemeInput{Name: "Synthetic hidden coverage", GroupID: 2, Enabled: true, Config: tc.config}
			assertCoverage := func(coverage PriceCoverage, missing []string) {
				t.Helper()
				if !slices.Equal(coverage.UncoveredModels, []string{"synthetic-new"}) || !slices.Equal(coverage.MissingCatalogPrices, missing) {
					t.Fatalf("coverage must include only visible models: %+v", coverage)
				}
			}
			scheme, err := manager.SaveScheme(ctx, 0, input)
			if err != nil {
				t.Fatal(err)
			}
			assertCoverage(scheme.PriceCoverage, nil)
			manager.pricing = pricing.NewStatic(map[string]pricing.Price{"synthetic-priced": price})
			updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
			if err != nil || updated.Next == nil {
				t.Fatal("could not schedule updated prices", updated, err)
			}
			assertCoverage(updated.PriceCoverage, []string{"synthetic-stale"})
			assertCoverage(updated.Next.PriceCoverage, []string{"synthetic-stale"})
			listed, err := manager.Schemes(ctx)
			if err != nil || len(listed) != 1 || listed[0].Next == nil {
				t.Fatal("could not read current and scheduled coverage", listed, err)
			}
			assertCoverage(listed[0].PriceCoverage, []string{"synthetic-stale"})
			assertCoverage(listed[0].Next.PriceCoverage, []string{"synthetic-stale"})
			for _, at := range []int64{now, updated.Next.EffectiveAt} {
				if _, err := Check(ctx, db.New(conn), scheme.ID, user, 2, account, "synthetic-hidden-priced", at, time.UTC); err != nil {
					t.Fatal("hidden model lost its saved price", err)
				}
				for _, model := range []string{"synthetic-new", "synthetic-hidden-new"} {
					request := Request{ID: "synthetic-unpriced", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: model, StartedAt: at}
					if err := Begin(ctx, db.New(conn), request, at, time.UTC); !errors.Is(err, ErrUnpriced) {
						t.Fatalf("unpriced %s request must remain blocked: %v", model, err)
					}
				}
			}
		})
	}
}

func TestPriceCoverageKeepsModelsVisibleOnAnyAllowedAccount(t *testing.T) {
	manager := NewWithPricing(nil, pricing.NewStatic(map[string]pricing.Price{}))
	for _, visibleAllowed := range []bool{false, true} {
		for _, visibleFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("visible allowed %t first %t", visibleAllowed, visibleFirst), func(t *testing.T) {
				catalog := poolModelCatalog{policy: groups.ModelPolicy{Restricted: true, Models: []string{"codex/synthetic-shared"}}}
				if visibleAllowed {
					catalog.policy.Models = append(catalog.policy.Models, "claude/synthetic-shared")
				}
				providers := []string{"codex", "claude"}
				if visibleFirst {
					slices.Reverse(providers)
				}
				seen := map[string]bool{}
				for _, provider := range providers {
					snapshot := accounts.Catalog{Models: []string{"synthetic-shared"}, UpdatedAt: 1, Source: "synthetic"}
					if provider == "codex" {
						snapshot.HiddenModels = []string{"synthetic-shared"}
					}
					raw, err := json.Marshal(snapshot)
					if err != nil {
						t.Fatal(err)
					}
					catalog.addAccount(provider, raw, 0, seen)
				}
				coverage := manager.priceCoverage(catalog, Revision{Config: Config{Mode: "amount"}})
				var want []string
				if visibleAllowed {
					want = []string{"synthetic-shared"}
				}
				if !slices.Equal(coverage.UncoveredModels, want) {
					t.Fatalf("coverage did not respect allowed visible routes: %+v", coverage)
				}
			})
		}
	}
}

func TestAutomaticPricesReuseSavedRateWhenCatalogPriceDisappears(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	}))
	input := SchemeInput{Name: "Synthetic initial", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}}
	scheme, err := manager.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{})
	input.Name = "Synthetic renamed"
	updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil {
		t.Fatal("existing allowance should remain editable", err)
	}
	if updated.Next == nil || len(updated.Next.Config.Rates) != 1 || updated.Next.Config.Rates[0] != scheme.Config.Rates[0] {
		t.Fatal("saved price was not retained", updated.Next)
	}
	if len(updated.Next.PriceCoverage.MissingCatalogPrices) != 1 || updated.Next.PriceCoverage.MissingCatalogPrices[0] != "synthetic-priced" {
		t.Fatal("missing current catalog price was not reported", updated.Next.PriceCoverage)
	}
}

func TestAutomaticPricesKeepSavedRateWhenAccountCatalogIsUnavailable(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	}))
	input := SchemeInput{Name: "Synthetic initial", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}}
	scheme, err := manager.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = NULL WHERE id = ?`, account); err != nil {
		t.Fatal(err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{})
	input.Name = "Synthetic renamed"
	updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Next == nil || !updated.Next.PriceCoverage.CatalogUnavailable || len(updated.Next.Config.Rates) != 1 {
		t.Fatal("unavailable catalog should retain usable rate and return warning", updated.Next)
	}
}

func TestPriceCoverageRefreshesWithoutSavingAndIsNotPersisted(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	}))
	scheme, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic live warning", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{})
	listed, err := manager.Schemes(ctx)
	if err != nil || len(listed) != 1 || len(listed[0].PriceCoverage.MissingCatalogPrices) != 1 {
		t.Fatal("current missing price did not appear without saving", listed, err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	})
	listed, err = manager.Schemes(ctx)
	if err != nil || len(listed[0].PriceCoverage.MissingCatalogPrices) != 0 {
		t.Fatal("resolved catalog warning did not clear without saving", listed, err)
	}
	var raw string
	if err := conn.QueryRow(`SELECT config FROM allocation_revisions WHERE scheme_id = ?`, scheme.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "price_coverage") || strings.Contains(raw, "missing_price_models") || strings.Contains(raw, "catalog_unavailable") {
		t.Fatal("transient price warning was persisted", raw)
	}
}

func TestAllocationPoolCatalogsBatchRespectsPoliciesAndTenant(t *testing.T) {
	_, conn, member, firstAccount := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-one","synthetic-hidden"],"updated_at":1,"source":"synthetic"}`, firstAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE account_groups SET restricted_models=1 WHERE id=2;
		INSERT INTO group_models(group_id,model) VALUES(2,'synthetic-one');
		INSERT INTO accounts(id,provider,name,account_id,status,credential,created_at,updated_at,models_snapshot)
		VALUES('synthetic-second-account','codex','Synthetic second','synthetic-second-subject','ready',x'01',1,1,
		'{"models":["synthetic-two"],"updated_at":1,"source":"synthetic"}')`); err != nil {
		t.Fatal(err)
	}
	secondPool, err := groups.New(conn).Save(ctx, 0, groups.Input{Name: "Synthetic second pool", Enabled: true, AccountIDs: []string{"synthetic-second-account"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := groups.New(conn).SetMemberGroups(ctx, member, []int64{2, secondPool.ID}); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-one": {Input: 1_000_000, Output: 2_000_000},
		"synthetic-two": {Input: 1_000_000, Output: 2_000_000},
	}))
	for _, poolID := range []int64{2, secondPool.ID} {
		if _, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: fmt.Sprintf("Synthetic pool %d", poolID), GroupID: poolID, Enabled: true, Config: Config{
			Mode: "amount", Period: "day", Members: []Share{{UserID: member, Limit: 1_000_000}},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO users(id,username,role,password_hash,enabled,created_at)
		VALUES(3,'synthetic-foreign-owner','member','synthetic-hash',1,1);
		INSERT INTO tenants(id,name,owner_user_id,created_at) VALUES(2,'Synthetic foreign workspace',3,1);
		INSERT INTO memberships(tenant_id,user_id,role,created_at) VALUES(2,3,'owner',1);
		INSERT INTO accounts(id,tenant_id,provider,name,account_id,status,credential,created_at,updated_at)
		VALUES('synthetic-foreign-account',2,'codex','Synthetic foreign account','synthetic-foreign-subject','ready',x'01',1,1)`); err != nil {
		t.Fatal(err)
	}
	foreignPool, err := groups.NewForTenant(conn, 2).Save(ctx, 0, groups.Input{Name: "Synthetic foreign pool", Enabled: true, AccountIDs: []string{"synthetic-foreign-account"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewForTenant(conn, 2).SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic foreign", GroupID: foreignPool.ID, Enabled: true, Config: Config{
		Mode: "tokens", Period: "day", Members: []Share{{UserID: 3, Limit: 100}},
	}}); err != nil {
		t.Fatal(err)
	}
	catalogs, err := loadAllocationPoolCatalogs(ctx, db.New(conn), 1)
	if err != nil || len(catalogs) != 2 {
		t.Fatal("batch catalogs crossed tenant boundary or omitted a pool", catalogs, err)
	}
	if !catalogs[2].policy.Restricted || len(catalogs[2].models) != 1 || catalogs[2].models[0] != "synthetic-one" {
		t.Fatal("restricted pool policy not applied", catalogs[2])
	}
	if len(catalogs[secondPool.ID].models) != 1 || catalogs[secondPool.ID].models[0] != "synthetic-two" {
		t.Fatal("models crossed pool boundary", catalogs[secondPool.ID])
	}
	listed, err := manager.Schemes(ctx)
	if err != nil || len(listed) != 2 || listed[0].GroupID != 2 || listed[1].GroupID != secondPool.ID {
		t.Fatal("batch-priced allowance list is incorrect", listed, err)
	}
}

func TestNewlyPricedModelStillNeedsARevision(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced","synthetic-new"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	}))
	input := SchemeInput{Name: "Synthetic coverage", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}}
	scheme, err := manager.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
		"synthetic-new":    {Input: 1_000_000, Output: 2_000_000},
	})
	listed, err := manager.Schemes(ctx)
	if err != nil || len(listed) != 1 || len(listed[0].PriceCoverage.UncoveredModels) != 1 || len(listed[0].PriceCoverage.MissingCatalogPrices) != 0 {
		t.Fatal("saved revision should still warn about its missing rate", listed, err)
	}
	input.Name = "Synthetic updated coverage"
	updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil || updated.Next == nil || len(updated.Next.PriceCoverage.UncoveredModels) != 0 {
		t.Fatal("new revision should cover newly priced model", updated.Next, err)
	}
}

func TestMissingCatalogPricePrefersScheduledRateOverCurrentRate(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["synthetic-priced"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 1_000_000, Output: 2_000_000},
	}))
	input := SchemeInput{Name: "Synthetic first", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}}
	scheme, err := manager.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{
		"synthetic-priced": {Input: 3_000_000, Output: 4_000_000},
	})
	input.Name = "Synthetic second"
	updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil || updated.Next == nil || updated.Next.Config.Rates[0].Input != 3_000_000 {
		t.Fatal("new catalog price was not scheduled", updated.Next, err)
	}
	manager.pricing = pricing.NewStatic(map[string]pricing.Price{})
	input.Name = "Synthetic third"
	updated, err = manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil || updated.Next == nil || updated.Next.Config.Rates[0].Input != 3_000_000 {
		t.Fatal("older active price replaced newer scheduled price", updated.Next, err)
	}
}

func TestNilPricingServiceAcceptsSubmittedRates(t *testing.T) {
	manager, _, user, _ := fixture(t)
	scheme, err := manager.SaveScheme(context.Background(), 0, SchemeInput{Name: "Synthetic direct prices", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
		Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 2_000_000}},
	}})
	if err != nil || len(scheme.Config.Rates) != 1 {
		t.Fatal("nil pricing should retain supplied test-seam rate", scheme, err)
	}
}

func TestEditBoundaryUsesCurrentDurationWhenDraftIsLonger(t *testing.T) {
	manager, _, user, _ := fixture(t)
	ctx := context.Background()
	anchor := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	manager.now = func() time.Time { return unix(anchor) }
	input := SchemeInput{Name: "Synthetic short window", GroupID: 2, Enabled: true, Config: Config{
		Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: 1_000_000}},
		Members: []Share{{UserID: user}}, Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 2_000_000}},
	}}
	scheme, err := manager.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return unix(anchor + 3600) }
	input.Config.Windows = []WindowCondition{{DurationSeconds: 30 * 86400, Limit: 1_000_000}}
	updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil || updated.Next == nil || updated.Next.EffectiveAt != anchor+5*3600 {
		t.Fatal("draft duration incorrectly delayed this edit", updated.Next, err)
	}
}

func TestLongWindowEditReportsActualEffectiveDate(t *testing.T) {
	manager, _, user, _ := fixture(t)
	ctx := context.Background()
	anchor := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	manager.now = func() time.Time { return unix(anchor) }
	input := SchemeInput{Name: "Synthetic long window", GroupID: 2, Enabled: true, Config: Config{
		Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 30 * 86400, Limit: 1_000_000}},
		Members: []Share{{UserID: user}}, Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 2_000_000}},
	}}
	scheme, err := manager.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return unix(anchor + 3600) }
	listed, err := manager.Schemes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].EditEffectiveAt != anchor+30*86400 {
		t.Fatal("edit preview did not match longest current window", listed)
	}
	input.Name = "Synthetic longer name"
	updated, err := manager.SaveScheme(ctx, scheme.ID, conditioned(t, manager, scheme.ID, input))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Next == nil || updated.Next.EffectiveAt != listed[0].EditEffectiveAt {
		t.Fatal("saved edit and preview disagree", updated.Next, listed[0].EditEffectiveAt)
	}
}

func TestAutomaticPricesSupportFullPoolCatalog(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	models := make([]string, 129)
	prices := make(map[string]pricing.Price, len(models))
	for index := range models {
		models[index] = fmt.Sprintf("synthetic-model-%03d", index)
		prices[models[index]] = pricing.Price{Input: 1_000_000, Output: 2_000_000}
	}
	snapshot, err := json.Marshal(map[string]any{"models": models, "updated_at": 1, "source": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`, snapshot, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(prices))
	scheme, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic large catalog", GroupID: 2, Enabled: true, Config: Config{
		Mode: "amount", Period: "day", Members: []Share{{UserID: user, Limit: 1_000_000}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scheme.Config.Rates) != len(models) {
		t.Fatalf("want %d saved model prices, got %d", len(models), len(scheme.Config.Rates))
	}
}

func TestRatioSchemeBuildsRatesFromPoolCatalog(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["gpt-5.1-codex","claude-3-7-sonnet-20250219"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{
		"gpt-5.1-codex":              {Input: 1250000, Cached: 125000, Output: 10000000},
		"claude-3-7-sonnet-20250219": {Input: 3000000, Cached: 300000, Output: 15000000},
	}))
	scheme, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic ratio", GroupID: 2, Enabled: true, Config: Config{Mode: "ratio", RatioUnit: "amount", Total: 1_000_000, Period: "month", Members: []Share{{UserID: user, Limit: 10000}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scheme.Config.Rates) != 2 || scheme.Config.Rates[0].Model != "claude-3-7-sonnet-20250219" || scheme.Config.Rates[1].Model != "gpt-5.1-codex" {
		t.Fatalf("automatic ratio rates not snapshotted: %+v", scheme.Config.Rates)
	}
}

func TestRatioSchemeRejectsUnpricedPoolModel(t *testing.T) {
	_, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec(`UPDATE accounts SET models_snapshot = ? WHERE id = ?`,
		`{"models":["unknown-codex-model"],"updated_at":1,"source":"synthetic"}`, account); err != nil {
		t.Fatal(err)
	}
	manager := NewWithPricing(conn, pricing.NewStatic(map[string]pricing.Price{}))
	_, err := manager.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic ratio", GroupID: 2, Enabled: true, Config: Config{Mode: "ratio", RatioUnit: "amount", Total: 1_000_000, Period: "month", Members: []Share{{UserID: user, Limit: 10000}}}})
	if !errors.Is(err, ErrUnpriced) {
		t.Fatalf("want unpriced model error, got %v", err)
	}
}

func TestCostSeparatesCacheAndPreservesSmallCharges(t *testing.T) {
	r := Rate{Model: "synthetic-model", Input: 2000000, Cached: 200000, Output: 8000000}
	cost, err := Cost(r, 100, 50, 40)
	if err != nil || cost != 528 {
		t.Fatal(cost, err)
	}
	cost, err = Cost(Rate{Input: 1, Cached: 1, Output: 1}, 1, 0, 0)
	if err != nil || cost != 1 {
		t.Fatal("lost fractional charge", cost, err)
	}
	if _, err = Cost(r, 10, 0, 11); !errors.Is(err, ErrInput) {
		t.Fatal("invalid cached subset", err)
	}
}
func TestSchemeRevisionDoesNotResetCurrentAllowance(t *testing.T) {
	s, conn, user, _ := fixture(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	input := SchemeInput{Name: "Synthetic scheme", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "month", Members: []Share{{UserID: user, Limit: 100}}}}
	scheme, err := s.SaveScheme(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Config.Mode = "amount"
	input.Config.Members[0].Limit = 200
	input.Config.Rates = []Rate{{Model: "synthetic-model", Input: 1, Cached: 1, Output: 1}}
	updated, err := s.SaveScheme(ctx, scheme.ID, conditioned(t, s, scheme.ID, input))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.Mode != "tokens" || updated.Next == nil || updated.Next.Config.Mode != "amount" {
		t.Fatal("mode changed current period", updated)
	}
	now = time.Date(2026, 9, 1, 0, 0, 1, 0, time.UTC)
	rev, err := Current(ctx, db.New(conn), scheme.ID, now.Unix())
	if err != nil || rev.Config.Mode != "amount" {
		t.Fatal(rev, err)
	}
}

func TestAccountingIsolationPendingRecoveryAndRollover(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := int64(1_900_000_000)
	// Scheme activation and admission must share a clock even when CI crosses a second.
	s.now = func() time.Time { return unix(now) }
	q := db.New(conn)
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic budget", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 5}}}})
	if err != nil {
		t.Fatal(err)
	}
	r := Request{ID: "synthetic-request", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic-model", StartedAt: now}
	if err = Begin(ctx, q, r, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err = Finish(ctx, q, r.ID, Completion{Input: 3, Output: 2, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err = Check(ctx, q, scheme.ID, user, 2, account, r.Model, now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("exhausted allowance admitted", err)
	}
	if _, err = Check(ctx, q, scheme.ID, user, 2, account, r.Model, now+86400, time.UTC); err != nil {
		t.Fatal("daily reset", err)
	}
	now += 86400
	r.ID = "synthetic-pending"
	r.StartedAt = now
	if err = Begin(ctx, q, r, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err = q.RecoverAllocationEntries(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if detail, err := s.Detail(ctx, scheme.ID, user); err != nil {
		t.Fatal(err)
	} else if balance := detail.Balances[0]; balance.Pending != 1 || balance.PendingCurrent != 1 || balance.Reserved != 0 || balance.Admission != "active" {
		t.Fatal("current-cycle pending exposure was not reported", balance)
	}
	now += 86400
	if _, err = Check(ctx, q, scheme.ID, user, 2, account, r.Model, now, time.UTC); err != nil {
		t.Fatal("previous cycle debt froze the new cycle", err)
	}
	if detail, err := s.Detail(ctx, scheme.ID, user); err != nil || len(detail.Pending) != 1 {
		t.Fatal("unsettled debt disappeared from the report", err)
	} else if balance := detail.Balances[0]; balance.Pending != 1 || balance.PendingCurrent != 0 || balance.InFlight != 0 || balance.Reserved != 0 || balance.Admission != "active" {
		t.Fatal("previous-cycle pending exposure blocked the new cycle", balance)
	}
	if err = s.Settle(ctx, scheme.ID, r.ID, Completion{Input: 3, Output: 2}); err != nil {
		t.Fatal(err)
	}
	if err = s.Settle(ctx, scheme.ID, r.ID, Completion{Input: 3, Output: 2}); err != nil {
		t.Fatal("settlement retry", err)
	}
	if _, err = Check(ctx, q, scheme.ID, user, 2, account, r.Model, now+172800, time.UTC); err != nil {
		t.Fatal("charged next period", err)
	}
}

func TestInFlightRequestsReserveRiskWithoutEarlyQuotaExhaustion(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic bounded risk", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	r := Request{ID: "synthetic-settled", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, r, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, r.ID, Completion{Input: 95, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	r.ID = "synthetic-inflight"
	if err := Begin(ctx, q, r, now, time.UTC); err != nil {
		t.Fatal("first in-flight request should fit the risk buffer", err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, r.Model, now, time.UTC); !errors.Is(err, ErrRisk) {
		t.Fatal("second request exceeded the reserved risk buffer", err)
	}
	if detail, err := s.Detail(ctx, scheme.ID, user); err != nil {
		t.Fatal(err)
	} else if balance := detail.Balances[0]; balance.InFlight != 1 || balance.PendingCurrent != 0 || balance.Reserved != 10 || balance.AdmissionRoom != 5 || balance.Admission != "risk_limited" {
		t.Fatal("balance disagreed with gateway risk admission", balance)
	}
	if err := Finish(ctx, q, r.ID, Completion{Input: 2, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, r.Model, now, time.UTC); err != nil {
		t.Fatal("reservation was not released", err)
	}
	if detail, err := s.Detail(ctx, scheme.ID, user); err != nil {
		t.Fatal(err)
	} else if balance := detail.Balances[0]; balance.InFlight != 0 || balance.Reserved != 0 || balance.AdmissionRoom != 13 || balance.Admission != "active" {
		t.Fatal("settled request still occupied risk allowance", balance)
	}
}

func TestUnsettledRequestCountHasAutomaticBound(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic active cap", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 1000}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	for i := range 4 {
		r := Request{ID: fmt.Sprintf("synthetic-active-%d", i), SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
		if err := Begin(ctx, q, r, now, time.UTC); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrRisk) {
		t.Fatal("fifth unsettled request was allowed", err)
	}
	if detail, err := s.Detail(ctx, scheme.ID, user); err != nil {
		t.Fatal(err)
	} else if balance := detail.Balances[0]; balance.InFlight != 4 || balance.AdmissionRoom != 700 || balance.Admission != "risk_limited" {
		t.Fatal("balance missed the unsettled-request count cap", balance)
	}
}

func TestPendingUsageDoesNotReserveAdmissionAfterRequestEnds(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic post-use accounting", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 1000}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	for i := range 4 {
		id := fmt.Sprintf("synthetic-pending-%d", i)
		r := Request{ID: id, SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
		if err := Begin(ctx, q, r, now, time.UTC); err != nil {
			t.Fatal(err)
		}
		if err := Finish(ctx, q, id, Completion{Dispatched: true}, now*1000, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("finished requests with unknown usage blocked admission", err)
	}
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Pending) != 4 || detail.Balances[0].PendingCurrent != 4 || detail.Balances[0].Reserved != 0 || detail.Balances[0].Admission != "active" {
		t.Fatal("pending usage still reserved capacity or disappeared", detail.Balances, detail.Pending)
	}
}

func TestPartialPendingUsageDoesNotChargeUntilConfirmed(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic partial pending", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 5}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	request := Request{ID: "synthetic-partial", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, request, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, request.ID, Completion{Input: 5, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	assertPendingDoesNotCharge := func() {
		t.Helper()
		if _, err := Check(ctx, q, scheme.ID, user, 2, account, request.Model, now, time.UTC); err != nil {
			t.Fatal("partial pending usage blocked admission", err)
		}
		detail, err := s.Detail(ctx, scheme.ID, user)
		if err != nil || len(detail.Pending) != 1 || detail.Pending[0].Input != 5 || detail.Balances[0].Used != 0 {
			t.Fatal("pending observations changed the charged balance", detail, err)
		}
	}
	assertPendingDoesNotCharge()
	// Existing databases can contain a nonzero cost on pending entries.
	if _, err := conn.ExecContext(ctx, "UPDATE allocation_entries SET cost=5 WHERE request_id=?", request.ID); err != nil {
		t.Fatal(err)
	}
	assertPendingDoesNotCharge()
	if err := s.Settle(ctx, scheme.ID, request.ID, Completion{Input: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, request.Model, now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("confirmed usage was not charged", err)
	}
}

func TestWindowedAmountChecksFiveHoursAndSevenDays(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	config := Config{Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: 100}, {DurationSeconds: 7 * 86400, Limit: 200}}, Members: []Share{{UserID: user}}, Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}}}
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic dual windows", GroupID: 2, Enabled: true, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	charge := func(id string, tokens int64) {
		t.Helper()
		r := Request{ID: id, SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
		if err := Begin(ctx, q, r, now, time.UTC); err != nil {
			t.Fatal("begin", id, err)
		}
		if err := Finish(ctx, q, id, Completion{Input: tokens, Known: true, Dispatched: true}, now*1000, false); err != nil {
			t.Fatal("finish", id, err)
		}
	}
	charge("synthetic-5h-first", 90)
	if detail, err := s.Detail(ctx, scheme.ID, user); err != nil {
		t.Fatal(err)
	} else if len(detail.Balances) != 2 || detail.Balances[0].Mode != "amount" || detail.Balances[0].WindowKind != "duration" || detail.Balances[0].WindowSeconds != 5*3600 || detail.Balances[0].Limit != 100 || detail.Balances[0].Used != 90 || detail.Balances[0].ResetAt != now+5*3600 || detail.Balances[1].Mode != "amount" || detail.Balances[1].WindowKind != "duration" || detail.Balances[1].WindowSeconds != 7*86400 || detail.Balances[1].Limit != 200 || detail.Balances[1].Used != 90 || detail.Balances[1].ResetAt != now+7*86400 {
		t.Fatal("dual balances do not share one settled request", detail.Balances)
	}
	charge("synthetic-5h-last", 10)
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("five-hour amount limit was exceeded", err)
	}
	now += 5 * 3600
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("five-hour reset did not reopen the weekly allowance", err)
	}
	charge("synthetic-7d-last", 100)
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("seven-day amount limit was exceeded", err)
	}
	now += 7*86400 - 5*3600
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("seven-day reset did not reopen the allowance", err)
	}
}

func TestWindowedAmountRejectsInvalidConditionsAndOverrides(t *testing.T) {
	s, _, user, _ := fixture(t)
	ctx := context.Background()
	base := Config{Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: 100}, {DurationSeconds: 7 * 86400, Limit: 200}}, Members: []Share{{UserID: user}}, Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}}}
	for _, invalid := range []Config{
		func() Config { c := base; c.Windows = nil; return c }(),
		func() Config {
			c := base
			c.Windows = []WindowCondition{{DurationSeconds: 3600, Limit: 100}, {DurationSeconds: 3600, Limit: 200}}
			return c
		}(),
		func() Config { c := base; c.Windows = []WindowCondition{{DurationSeconds: 3599, Limit: 100}}; return c }(),
		func() Config { c := base; c.Windows = []WindowCondition{{DurationSeconds: 3601, Limit: 100}}; return c }(),
		func() Config {
			c := base
			c.Windows = []WindowCondition{{DurationSeconds: 366 * 86400, Limit: 100}}
			return c
		}(),
		func() Config { c := base; c.Windows = []WindowCondition{{DurationSeconds: 3600, Limit: -1}}; return c }(),
		func() Config {
			c := base
			c.Members = []Share{{UserID: user, WindowOverrides: []MemberWindowLimit{{DurationSeconds: 5 * 3600, Limit: -1}}}}
			return c
		}(),
		func() Config {
			c := base
			c.Members = []Share{{UserID: user, WindowOverrides: []MemberWindowLimit{{DurationSeconds: 5 * 3600, Limit: 100}, {DurationSeconds: 5 * 3600, Limit: 200}}}}
			return c
		}(),
		func() Config { c := base; c.Total = 100; return c }(),
		func() Config { c := base; c.ResetTime = "09:00"; return c }(),
		func() Config { c := base; c.Period = "day"; return c }(),
		func() Config {
			c := base
			c.Windows = make([]WindowCondition, 9)
			for i := range c.Windows {
				c.Windows[i] = WindowCondition{DurationSeconds: int64(i+1) * 3600, Limit: 100}
			}
			return c
		}(),
		func() Config {
			c := base
			c.Mode = "amount"
			c.Period = "day"
			return c
		}(),
	} {
		if _, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic dual", GroupID: 2, Enabled: true, Config: invalid}); !errors.Is(err, ErrInput) {
			t.Fatal("invalid windowed amount accepted", invalid, err)
		}
	}
}

func TestUnknownWindowOverrideReturnsSpecificError(t *testing.T) {
	manager, _, user, _ := fixture(t)
	_, err := manager.SaveScheme(context.Background(), 0, SchemeInput{Name: "Synthetic stale override", GroupID: 2, Enabled: true, Config: Config{
		Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: 100}},
		Members: []Share{{UserID: user, WindowOverrides: []MemberWindowLimit{{DurationSeconds: 7 * 86400, Limit: 100}}}},
		Rates:   []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}},
	}})
	if !errors.Is(err, ErrUnknownWindowOverride) {
		t.Fatal("want unknown-window-override error, got", err)
	}
}

func TestWindowedAmountPendingExposureFollowsEachWindow(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic dual pending", GroupID: 2, Enabled: true, Config: Config{
		Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: 100}, {DurationSeconds: 7 * 86400, Limit: 200}}, Members: []Share{{UserID: user}},
		Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	r := Request{ID: "synthetic-unmetered", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, r, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, r.ID, Completion{Known: false, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	now += 5 * 3600
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil || len(detail.Balances) != 2 {
		t.Fatal(err, detail.Balances)
	}
	if detail.Balances[0].PendingCurrent != 0 || detail.Balances[0].Reserved != 0 || detail.Balances[1].PendingCurrent != 1 || detail.Balances[1].Reserved != 0 {
		t.Fatal("weekly pending usage reserved capacity or disappeared", detail.Balances)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("one weekly pending request froze both windows", err)
	}
}

func TestWindowedAmountChangesStartAtLongestWindowBoundary(t *testing.T) {
	s, _, user, _ := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	start := now
	s.now = func() time.Time { return unix(now) }
	config := Config{Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 2 * 3600, Limit: 100}, {DurationSeconds: 3 * 86400, Limit: 200}}, Members: []Share{{UserID: user}}, Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}}}
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic dual revisions", GroupID: 2, Enabled: true, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	now += 3600
	config.Windows = []WindowCondition{{DurationSeconds: 2 * 3600, Limit: 150}, {DurationSeconds: 3 * 86400, Limit: 200}}
	updated, err := s.SaveScheme(ctx, scheme.ID, conditioned(t, s, scheme.ID, SchemeInput{Name: scheme.Name, GroupID: 2, Enabled: true, Config: config}))
	if err != nil || updated.Next == nil {
		t.Fatal(err, updated)
	}
	if updated.Config.Windows[0].Limit != 100 || updated.Next.Config.Windows[0].Limit != 150 || updated.Next.EffectiveAt != start+3*86400 {
		t.Fatal("condition edit changed an active long window", updated)
	}
}

func TestWindowedAmountAllowsEachLimitToBeUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name          string
		five, seven   int64
		wantFive      string
		wantSeven     string
		wantAdmission error
	}{
		{name: "five only", five: 100, seven: 0, wantFive: "exhausted", wantSeven: "unlimited", wantAdmission: ErrQuota},
		{name: "seven only", five: 0, seven: 200, wantFive: "unlimited", wantSeven: "exhausted", wantAdmission: ErrQuota},
		{name: "both unlimited", five: 0, seven: 0, wantFive: "unlimited", wantSeven: "unlimited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, conn, user, account := fixture(t)
			ctx := context.Background()
			now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
			s.now = func() time.Time { return unix(now) }
			scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic optional windows", GroupID: 2, Enabled: true, Config: Config{
				Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: tc.five}, {DurationSeconds: 7 * 86400, Limit: tc.seven}}, Members: []Share{{UserID: user}},
				Rates: []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			q := db.New(conn)
			r := Request{ID: "synthetic-usage", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
			if err := Begin(ctx, q, r, now, time.UTC); err != nil {
				t.Fatal(err)
			}
			if err := Finish(ctx, q, r.ID, Completion{Input: 250, Known: true, Dispatched: true}, now*1000, false); err != nil {
				t.Fatal(err)
			}
			_, err = Check(ctx, q, scheme.ID, user, 2, account, r.Model, now, time.UTC)
			if tc.wantAdmission == nil && err != nil || tc.wantAdmission != nil && !errors.Is(err, tc.wantAdmission) {
				t.Fatal("wrong unlimited admission", err)
			}
			detail, err := s.Detail(ctx, scheme.ID, user)
			if err != nil || len(detail.Balances) != 2 {
				t.Fatal(err, detail.Balances)
			}
			if detail.Balances[0].Admission != tc.wantFive || detail.Balances[1].Admission != tc.wantSeven || detail.Balances[0].Used != 250 || detail.Balances[1].Used != 250 {
				t.Fatal("unlimited balance lost recorded usage or changed status", detail.Balances)
			}
		})
	}
}

func TestWindowedSharedDefaultsAndMemberOverrideKeepSeparateLedgers(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	s.now = func() time.Time { return unix(now) }
	identity, err := auth.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	second, err := identity.CreateMember(ctx, "synthetic-second", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	third, err := identity.CreateMember(ctx, "synthetic-third", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := grantPoolMembers(ctx, conn, 2, second.ID, third.ID); err != nil {
		t.Fatal(err)
	}
	config := Config{
		Mode: "windows", Period: "durations", Windows: []WindowCondition{{DurationSeconds: 5 * 3600, Limit: 100}, {DurationSeconds: 7 * 86400, Limit: 200}},
		Members: []Share{{UserID: user}, {UserID: second.ID}, {UserID: third.ID, WindowOverrides: []MemberWindowLimit{{DurationSeconds: 5 * 3600, Limit: 300}, {DurationSeconds: 7 * 86400, Limit: 0}}}},
		Rates:   []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}},
	}
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic shared windows", GroupID: 2, Enabled: true, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	first := Request{ID: "synthetic-first", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, first, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, first.ID, Completion{Input: 100, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("first member did not inherit five-hour limit", err)
	}
	if _, err := Check(ctx, q, scheme.ID, second.ID, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("one member's usage blocked another inherited ledger", err)
	}
	secondDetail, err := s.Detail(ctx, scheme.ID, second.ID)
	if err != nil || len(secondDetail.Balances) != 2 || secondDetail.Balances[0].Limit != 100 || secondDetail.Balances[0].Used != 0 || secondDetail.Balances[1].Limit != 200 {
		t.Fatal("second member did not inherit shared limits", secondDetail.Balances, err)
	}
	thirdRequest := Request{ID: "synthetic-third-request", SchemeID: scheme.ID, UserID: third.ID, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
	if err := Begin(ctx, q, thirdRequest, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := Finish(ctx, q, thirdRequest.ID, Completion{Input: 250, Known: true, Dispatched: true}, now*1000, false); err != nil {
		t.Fatal(err)
	}
	thirdDetail, err := s.Detail(ctx, scheme.ID, third.ID)
	if err != nil || len(thirdDetail.Balances) != 2 || thirdDetail.Balances[0].Limit != 300 || thirdDetail.Balances[0].Used != 250 || thirdDetail.Balances[1].Admission != "unlimited" {
		t.Fatal("personal override did not replace shared limits", thirdDetail.Balances, err)
	}
}

func TestDynamicWindowConditionsUseEachCustomDuration(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 4, 15, 12, 0, 0, 0, time.UTC).Unix()
	start := now
	s.now = func() time.Time { return unix(now) }
	config := Config{
		Mode: "windows", Period: "durations",
		Windows: []WindowCondition{{DurationSeconds: 2 * 3600, Limit: 100}, {DurationSeconds: 3 * 86400, Limit: 200}},
		Members: []Share{{UserID: user}},
		Rates:   []Rate{{Model: "synthetic", Input: 1_000_000, Output: 1_000_000}},
	}
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic custom windows", GroupID: 2, Enabled: true, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	charge := func(id string, cost int64) {
		t.Helper()
		r := Request{ID: id, SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}
		if err := Begin(ctx, q, r, now, time.UTC); err != nil {
			t.Fatal(err)
		}
		if err := Finish(ctx, q, id, Completion{Input: cost, Known: true, Dispatched: true}, now*1000, false); err != nil {
			t.Fatal(err)
		}
	}
	charge("synthetic-custom-first", 100)
	entry, err := q.GetAllocationEntry(ctx, "synthetic-custom-first")
	if err != nil || entry.ResetAt != start+3*86400 {
		t.Fatal("retention marker ended before the longest window", entry.ResetAt, err)
	}
	detail, err := s.Detail(ctx, scheme.ID, user)
	if err != nil || len(detail.Balances) != 2 || detail.Balances[0].WindowSeconds != 2*3600 || detail.Balances[0].Used != 100 || detail.Balances[1].WindowSeconds != 3*86400 || detail.Balances[1].Used != 100 {
		t.Fatal("custom windows did not count the same request", detail.Balances, err)
	}
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("short custom window did not enforce its limit", err)
	}
	now = start + 2*3600
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("short custom window did not renew", err)
	}
	charge("synthetic-custom-second", 100)
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); !errors.Is(err, ErrQuota) {
		t.Fatal("long custom window did not enforce its limit", err)
	}
	now = start + 3*86400
	if _, err := Check(ctx, q, scheme.ID, user, 2, account, "synthetic", now, time.UTC); err != nil {
		t.Fatal("long custom window did not renew", err)
	}
}

func TestManagedPoolRejectsNewAccountsAndCanPauseWithoutUpstreamQuota(t *testing.T) {
	s, conn, user, _ := fixture(t)
	ctx := context.Background()
	in := SchemeInput{Name: "Synthetic", GroupID: 2, Enabled: true, Config: Config{Mode: "ratio", RatioUnit: "tokens", Total: 1_000_000, Period: "month", Members: []Share{{UserID: user, Limit: 10000}}}}
	scheme, err := s.SaveScheme(ctx, 0, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetEnabled(ctx, scheme.ID, false); err != nil {
		t.Fatal("cannot pause during quota outage", err)
	}
	if _, err = conn.Exec("INSERT INTO accounts(id,name,provider,account_id,credential,email,plan,enabled,status,created_at,updated_at,models_revision) VALUES('synthetic-new','Synthetic','claude','synthetic-new',x'00','','',1,'ready',1,1,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec("INSERT INTO group_accounts(group_id,account_id) VALUES(2,'synthetic-new')"); err == nil {
		t.Fatal("managed default pool silently gained account")
	}
}

func TestFirstPoolCanBeReserved(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	pools := groups.New(conn)
	if _, err := pools.Save(ctx, 2, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pools.Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{account}}); err != nil {
		t.Fatal(err)
	}
	if err := pools.SetMemberGroups(ctx, user, []int64{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic", GroupID: 1, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}}); err != nil {
		t.Fatal("first pool cannot be reserved", err)
	}
	cipher, err := vault.Open(filepath.Join(t.TempDir(), "synthetic-import-key"), true)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := accounts.New(conn, cipher).Authorize(ctx, "Synthetic later import", accounts.Credential{AccountID: "synthetic-later", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal("reserving the first pool blocked later imports", err)
	}
	var count int
	if err = conn.QueryRow("SELECT count(*) FROM group_accounts WHERE account_id=?", imported.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("later import entered a reserved pool", err)
	}

}

func TestTokenCorrectionRejectsOversizedUsage(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	now := time.Now().Unix()
	if err = Begin(ctx, q, Request{ID: "synthetic-large", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now}, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err = q.RecoverAllocationEntries(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.Settle(ctx, scheme.ID, "synthetic-large", Completion{Input: 1_000_000_001}); !errors.Is(err, ErrInput) {
		t.Fatal("oversized manual usage accepted", err)
	}
}

func TestRetentionKeepsUnresolvedDebtAndCurrentBalances(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Now().Unix()
	s.now = func() time.Time { return unix(now - 100*86400) }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	for _, item := range []struct {
		id    string
		at    int64
		known bool
	}{{"synthetic-old", now - 100*86400, true}, {"synthetic-recent", now, true}, {"synthetic-debt", now - 100*86400, false}} {
		if err = Begin(ctx, q, Request{ID: item.id, SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: item.at}, item.at, time.UTC); err != nil {
			t.Fatal(err)
		}
		if err = Finish(ctx, q, item.id, Completion{Input: 1, Known: item.known, Dispatched: true}, item.at*1000, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = Prune(ctx, q, now-90*86400); err != nil {
		t.Fatal(err)
	}
	if _, err = q.GetAllocationEntry(ctx, "synthetic-old"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old entry retained", err)
	}
	for _, id := range []string{"synthetic-recent", "synthetic-debt"} {
		if _, err = q.GetAllocationEntry(ctx, id); err != nil {
			t.Fatal("lost balance or debt", id, err)
		}
	}
}

func TestPersonalReportFiltersBeforePendingLimit(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	if _, err := conn.Exec("INSERT INTO users(id,username,role,password_hash,enabled,created_at) VALUES(3,'synthetic-other','member','synthetic',1,1)"); err != nil {
		t.Fatal(err)
	}
	if err := grantPoolMembers(ctx, conn, 2, 3); err != nil {
		t.Fatal(err)
	}
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}, {UserID: 3, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := Current(ctx, db.New(conn), scheme.ID, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 257; i++ {
		uid := int64(3)
		if i == 256 {
			uid = user
		}
		if _, err = conn.Exec("INSERT INTO allocation_entries(request_id,scheme_id,revision_id,user_id,account_id,model,mode,window_start,reset_at,started_at,state) VALUES(?,?,?,?,?,'synthetic','tokens',0,1,?,'pending')", fmt.Sprintf("synthetic-%03d", i), scheme.ID, rev.ID, uid, account, i); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Own(ctx, user)
	if err != nil || len(result) != 1 {
		t.Fatal(result, err)
	}
	report := result[0]
	if len(report.Pending) != 1 || report.Pending[0].UserID != user {
		t.Fatal("own pending debt hidden by other members", report.Pending)
	}
	if len(report.Config.Members) != 1 || report.Config.Members[0].UserID != user || len(report.Balances) != 1 || report.Balances[0].UserID != user {
		t.Fatal("another member exposed", report)
	}
	admin, err := s.Detail(ctx, scheme.ID, 0)
	if err != nil || len(admin.Pending) != 256 || admin.Pending[0].RequestID != "synthetic-256" {
		t.Fatal("recent pending usage hidden behind old debt", err)
	}
}

func TestManagedAccountDeletionExplainsPoolLock(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	_, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.New(conn, nil).Delete(ctx, account); !errors.Is(err, accounts.ErrAllocated) {
		t.Fatal("missing actionable lock error", err)
	}
}

func TestCostSettlesImageInputByModality(t *testing.T) {
	// Rates are micro-USD per million tokens: text input $5/M, image input $8/M, image output $30/M.
	rate := Rate{Input: 5_000_000, Cached: 1_250_000, Output: 30_000_000, ImageInput: 8_000_000}
	cost, err := CostWithImages(rate, 1_000_000, 400_000, 1_000_000, 100_000)
	// 500k text * 5 + 400k image * 8 + 100k cached * 1.25 + 1M output * 30 = 2.5 + 3.2 + 0.125 + 30 USD.
	if err != nil || cost != 35_825_000 {
		t.Fatal(cost, err)
	}
	if plain, err := Cost(rate, 1_000_000, 1_000_000, 100_000); err != nil || plain != 34_625_000 {
		t.Fatal("text-only cost changed", plain, err)
	}
	fallback := rate
	fallback.ImageInput = 0
	if cost, err := CostWithImages(fallback, 1_000_000, 400_000, 0, 0); err != nil || cost != 5_000_000 {
		t.Fatal("missing image rate must use the input rate", cost, err)
	}
	if _, err := CostWithImages(rate, 100, 60, 0, 50); !errors.Is(err, ErrInput) {
		t.Fatal("overlapping categories accepted", err)
	}
}

func TestManualSettlementChargesImageInputAtImageRate(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	config := `{"mode":"amount","period":"day","members":[{"user_id":` + strconv.FormatInt(user, 10) + `,"limit":1000000000}],"rates":[{"model":"gpt-image-2","input":5000000,"cached":1250000,"output":30000000,"image_input":8000000}]}`
	if _, err := conn.Exec(`INSERT INTO allocation_schemes(id,name,group_id,enabled,created_at) VALUES(9,'Synthetic image allowance',2,1,?)`, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO allocation_revisions(id,scheme_id,effective_at,accounting_anchor_at,config) VALUES(90,9,?,?,?)`, now.Unix()-60, now.Unix()-60, config); err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	r := Request{ID: "synthetic-image-edit", SchemeID: 9, UserID: user, GroupID: 2, AccountID: account, Model: "gpt-image-2", StartedAt: now.Unix()}
	if err := Begin(ctx, q, r, now.Unix(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := q.RecoverAllocationEntries(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle(ctx, 9, r.ID, Completion{Input: 1_000_000, ImageInput: 400_000, Cached: 100_000, Output: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	var cost int64
	if err := conn.QueryRow(`SELECT cost FROM allocation_entries WHERE request_id=?`, r.ID).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	// 500k text * $5 + 400k image * $8 + 100k cached * $1.25 + 1M image output * $30, in micro-USD.
	if cost != 35_825_000 {
		t.Fatalf("manual settlement ignored the image input split: %d", cost)
	}
	// An identical retry succeeds; the same totals with another image split are a conflict.
	if err := s.Settle(ctx, 9, r.ID, Completion{Input: 1_000_000, ImageInput: 400_000, Cached: 100_000, Output: 1_000_000}); err != nil {
		t.Fatal("identical settlement retry failed", err)
	}
	if err := s.Settle(ctx, 9, r.ID, Completion{Input: 1_000_000, ImageInput: 600_000, Cached: 100_000, Output: 1_000_000}); !errors.Is(err, ErrSettlement) {
		t.Fatal("a different image split was treated as a retry", err)
	}
	var stored, image int64
	if err := conn.QueryRow(`SELECT cost, image_input_tokens FROM allocation_entries WHERE request_id=?`, r.ID).Scan(&stored, &image); err != nil || stored != 35_825_000 || image != 400_000 {
		t.Fatal("settlement details changed", stored, image, err)
	}
	overlap := r
	overlap.ID = "synthetic-image-overlap"
	if err := Begin(ctx, q, overlap, now.Unix(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := q.RecoverAllocationEntries(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle(ctx, 9, overlap.ID, Completion{Input: 100, ImageInput: 60, Cached: 50}); !errors.Is(err, ErrInput) {
		t.Fatal("overlapping image and cached input accepted", err)
	}
}
