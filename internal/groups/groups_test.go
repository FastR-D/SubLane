package groups_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/vault"
)

func fixture(t *testing.T) (*sql.DB, *groups.Service, auth.Member, accounts.Account) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	connection, err := storage.Open(ctx, filepath.Join(dir, "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(dir, "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	account, err := accounts.New(connection, cipher).Authorize(ctx, "Synthetic", accounts.Credential{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", AccountID: "synthetic-subject", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	return connection, groups.New(connection), member, account
}

func TestNewAccountsAndMembersRemainUnassigned(t *testing.T) {
	connection, service, member, _ := fixture(t)
	ctx := context.Background()
	choices, err := service.Available(ctx, member.ID)
	if err != nil || len(choices) != 0 {
		t.Fatalf("unexpected automatic access: %+v, %v", choices, err)
	}
	for _, table := range []string{"group_accounts", "group_members"} {
		var count int
		if err := connection.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("automatic membership in %s: %d, %v", table, count, err)
		}
	}
}

func TestPoolManagementAndGrantsStayInWorkspace(t *testing.T) {
	connection, first, member, firstAccount := fixture(t)
	ctx := context.Background()
	if _, err := connection.Exec(`INSERT INTO users(id,username,role,password_hash,enabled,created_at)
		VALUES(3,'synthetic-owner-two','member','synthetic-hash',1,1);
		INSERT INTO tenants(id,name,owner_user_id,created_at) VALUES(2,'Second workspace',3,1);
		INSERT INTO memberships(tenant_id,user_id,role,created_at)
		VALUES(2,3,'owner',1),(2,2,'member',1);
		INSERT INTO accounts(id,tenant_id,provider,name,account_id,status,credential,created_at,updated_at)
		VALUES('second-account',2,'codex','Second account','synthetic-subject','ready',x'01',1,1)`); err != nil {
		t.Fatal(err)
	}
	second := groups.NewForTenant(connection, 2)
	firstPool, err := first.Save(ctx, 0, groups.Input{Name: "Shared name", Enabled: true, AccountIDs: []string{firstAccount.ID}})
	if err != nil {
		t.Fatal(err)
	}
	secondPool, err := second.Save(ctx, 0, groups.Input{Name: "Shared name", Enabled: true, AccountIDs: []string{"second-account"}})
	if err != nil {
		t.Fatalf("second workspace could not use its own pool name: %v", err)
	}
	if _, err := second.Save(ctx, firstPool.ID, groups.Input{Name: "Foreign edit", Enabled: true, AccountIDs: []string{"second-account"}}); !errors.Is(err, groups.ErrNotFound) {
		t.Fatalf("foreign workspace pool was editable: %v", err)
	}
	if _, err := second.Save(ctx, 0, groups.Input{Name: "Foreign account", Enabled: true, AccountIDs: []string{firstAccount.ID}}); !errors.Is(err, groups.ErrInput) {
		t.Fatalf("foreign workspace account was assignable: %v", err)
	}
	for _, check := range []struct {
		service *groups.Service
		own     int64
		other   int64
	}{{first, firstPool.ID, secondPool.ID}, {second, secondPool.ID, firstPool.ID}} {
		values, err := check.service.List(ctx)
		if err != nil || len(values) != 1 || values[0].ID != check.own {
			t.Fatalf("pool list leaked another workspace: %+v, %v", values, err)
		}
		if _, err := check.service.Get(ctx, check.other); !errors.Is(err, groups.ErrNotFound) {
			t.Fatalf("foreign pool was readable: %v", err)
		}
	}
	if err := first.SetMemberGroups(ctx, member.ID, []int64{firstPool.ID}); err != nil {
		t.Fatal(err)
	}
	if err := second.SetMemberGroups(ctx, member.ID, []int64{secondPool.ID}); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		service *groups.Service
		want    int64
	}{{first, firstPool.ID}, {second, secondPool.ID}} {
		ids, err := check.service.MemberGroups(ctx, member.ID)
		if err != nil || len(ids) != 1 || ids[0] != check.want {
			t.Fatalf("pool grants crossed workspaces: %v, %v", ids, err)
		}
	}
	if err := second.SetMemberGroups(ctx, member.ID, []int64{}); err != nil {
		t.Fatal(err)
	}
	ids, err := first.MemberGroups(ctx, member.ID)
	if err != nil || len(ids) != 1 || ids[0] != firstPool.ID {
		t.Fatalf("revoking second workspace deleted first workspace grant: %v, %v", ids, err)
	}
}

func TestPoolRosterUsesActiveDirectGrantsAcrossWorkspaceRoles(t *testing.T) {
	connection, _, owner, _ := fixture(t)
	ctx := context.Background()
	workspace, err := tenants.New(connection).Create(ctx, owner.ID, "Second workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := tenants.New(connection).AddMember(ctx, owner.ID, workspace.ID, 1, tenants.RoleMember); err != nil {
		t.Fatal(err)
	}
	service := groups.NewForTenant(connection, workspace.ID)
	pool, err := service.Save(ctx, 0, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetMemberGroups(ctx, 1, []int64{pool.ID}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetMemberGroups(ctx, owner.ID, []int64{pool.ID}); err != nil {
		t.Fatal(err)
	}
	assertRoster := func(want int) {
		t.Helper()
		members, err := service.Members(ctx, pool.ID)
		if err != nil || len(members) != want {
			t.Fatalf("workspace member roster: %+v, %v", members, err)
		}
		listed, err := service.List(ctx)
		if err != nil || len(listed) != 1 || listed[0].MemberCount != int64(want) {
			t.Fatalf("workspace member count: %+v, %v", listed, err)
		}
	}
	assertRoster(2)
	if _, err := tenants.New(connection).SetMemberEnabled(ctx, owner.ID, workspace.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertRoster(1)
}

func TestFirstPoolCanBeRenamedAndDisabled(t *testing.T) {
	_, service, _, account := fixture(t)
	ctx := context.Background()
	pools, err := service.List(ctx)
	if err != nil || len(pools) != 0 {
		t.Fatalf("fresh instance created a pool: %+v, %v", pools, err)
	}
	pool, err := service.Save(ctx, 0, groups.Input{Name: "Synthetic first pool", Enabled: true, AccountIDs: []string{account.ID}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Save(ctx, pool.ID, groups.Input{Name: "Synthetic renamed", Enabled: false, AccountIDs: []string{account.ID}})
	if err != nil || updated.Name != "Synthetic renamed" || updated.Enabled || updated.IsDefault {
		t.Fatalf("first pool still special: %+v, %v", updated, err)
	}
}

func TestPoolAndMemberChangesAreAtomicAndScoped(t *testing.T) {
	_, service, member, account := assignedFixture(t)
	ctx := context.Background()
	pool, err := service.Save(ctx, 0, groups.Input{Name: "Project alpha", Enabled: true, AccountIDs: []string{account.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, 0, groups.Input{Name: "project alpha", Enabled: true, AccountIDs: []string{}}); !errors.Is(err, groups.ErrDuplicate) {
		t.Fatal("duplicate pool accepted", err)
	}
	if _, err := service.Save(ctx, pool.ID, groups.Input{Name: "Changed", Enabled: true, AccountIDs: []string{"missing-account"}}); !errors.Is(err, groups.ErrInput) {
		t.Fatal("missing account accepted", err)
	}
	value, err := service.Get(ctx, pool.ID)
	if err != nil || value.Name != "Project alpha" || len(value.AccountIDs) != 1 {
		t.Fatal("failed update changed pool", err)
	}
	if err := service.SetMemberGroups(ctx, member.ID, []int64{pool.ID}); err != nil {
		t.Fatal(err)
	}
	choices, err := service.Available(ctx, member.ID)
	if err != nil || len(choices) != 1 || choices[0].ID != pool.ID {
		t.Fatal("grant does not constrain choices", err)
	}
	if err := service.SetMemberGroups(ctx, member.ID, []int64{999}); !errors.Is(err, groups.ErrInput) {
		t.Fatal("missing group accepted", err)
	}
	ids, err := service.MemberGroups(ctx, member.ID)
	if err != nil || len(ids) != 1 || ids[0] != pool.ID {
		t.Fatal("failed grant update removed access", err)
	}
	if _, err := service.Save(ctx, pool.ID, groups.Input{Name: pool.Name, Enabled: false, AccountIDs: []string{account.ID}}); err != nil {
		t.Fatal(err)
	}
	choices, err = service.Available(ctx, member.ID)
	if err != nil || len(choices) != 0 {
		t.Fatal("disabled pool visible", err)
	}
	if err := service.SetMemberGroups(ctx, member.ID, []int64{}); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionReadinessOnlyUsesAuthorizedGroups(t *testing.T) {
	_, service, member, _ := assignedFixture(t)
	ctx := context.Background()
	status, err := service.Connection(ctx, member.ID)
	if err != nil || status != "ready" {
		t.Fatal(status, err)
	}
	empty, err := service.Save(ctx, 0, groups.Input{Name: "Empty", Enabled: true, AccountIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetMemberGroups(ctx, member.ID, []int64{empty.ID}); err != nil {
		t.Fatal(err)
	}
	status, err = service.Connection(ctx, member.ID)
	if err != nil || status != "not_configured" {
		t.Fatal("another pool leaked readiness", status, err)
	}
}

func TestConnectionReadinessIncludesEnabledSubscriptionProviders(t *testing.T) {
	connection, service, member, account := fixture(t)
	ctx := context.Background()
	if _, err := connection.ExecContext(ctx, "UPDATE accounts SET provider='claude' WHERE id=?", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, 0, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{account.ID}}); err != nil {
		t.Fatal(err)
	}
	choices, err := service.Available(ctx, 1)
	if err != nil || len(choices) != 1 || choices[0].AccountCount != 1 {
		t.Fatal("Claude account missing from available pool", choices, err)
	}
	status, err := service.Connection(ctx, 1)
	if err != nil || status != "ready" {
		t.Fatal("Claude pool was not ready", status, err)
	}
	status, err = service.Connection(ctx, member.ID)
	if err != nil || status != "not_configured" {
		t.Fatal("ungranted pool made member ready", status, err)
	}
	if _, err := connection.ExecContext(ctx, "UPDATE accounts SET provider='antigravity' WHERE id=?", account.ID); err != nil {
		t.Fatal(err)
	}
	status, err = service.Connection(ctx, 1)
	if err != nil || status != "ready" {
		t.Fatal("Antigravity pool was not ready", status, err)
	}
	if _, err := connection.ExecContext(ctx, "UPDATE accounts SET enabled=0 WHERE id=?", account.ID); err != nil {
		t.Fatal(err)
	}
	status, err = service.Connection(ctx, 1)
	if err != nil || status != "not_configured" {
		t.Fatal("disabled subscription made pool ready", status, err)
	}
}

func TestPoolMembersIncludesEveryoneWithEffectiveAccess(t *testing.T) {
	connection, service, member, _ := assignedFixture(t)
	ctx := context.Background()
	assertMembers := func(want ...int64) {
		t.Helper()
		values, err := service.Members(ctx, 1)
		if err != nil || len(values) != len(want) {
			t.Fatalf("pool members: %+v, %v", values, err)
		}
		for i, id := range want {
			if values[i].ID != id {
				t.Fatalf("pool members: %+v, want IDs %v", values, want)
			}
		}
		listed, err := service.List(ctx)
		if err != nil || len(listed) != 1 || listed[0].MemberCount != int64(len(want)) {
			t.Fatalf("pool member count: %+v, %v", listed, err)
		}
	}
	assertMembers(1, member.ID)
	if err := service.SetMemberGroups(ctx, member.ID, []int64{}); err != nil {
		t.Fatal(err)
	}
	assertMembers(1)
	if _, err := tenants.New(connection).SetMemberRole(ctx, 1, 1, member.ID, tenants.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	assertMembers(1, member.ID)
}

func assignedFixture(t *testing.T) (*sql.DB, *groups.Service, auth.Member, accounts.Account) {
	conn, service, member, account := fixture(t)
	pool, err := service.Save(context.Background(), 0, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{account.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetMemberGroups(context.Background(), member.ID, []int64{pool.ID}); err != nil {
		t.Fatal(err)
	}
	return conn, service, member, account
}

func TestPreferenceBoostsStayWithGroupMembership(t *testing.T) {
	connection, service, _, account := fixture(t)
	ctx := context.Background()
	save := func(id int64, name string, ids []string, boosts map[string]int64) groups.Detail {
		t.Helper()
		g, err := service.Save(ctx, id, groups.Input{Name: name, Enabled: true, AccountIDs: ids, PreferenceBoosts: boosts})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	first := save(0, "First", []string{account.ID}, map[string]int64{account.ID: 150})
	second := save(0, "Second", []string{account.ID}, nil)
	if first.PreferenceBoosts[account.ID] != 150 || second.PreferenceBoosts[account.ID] != 0 {
		t.Fatal(first, second)
	}
	first = save(first.ID, "Renamed", []string{account.ID}, nil)
	if first.PreferenceBoosts[account.ID] != 150 {
		t.Fatal("legacy edit erased boost", first)
	}
	for _, boosts := range []map[string]int64{{account.ID: -1}, {account.ID: 301}, {"foreign-account": 50}} {
		if _, err := service.Save(ctx, first.ID, groups.Input{Name: "Invalid", Enabled: true, AccountIDs: []string{account.ID}, PreferenceBoosts: boosts}); !errors.Is(err, groups.ErrInput) {
			t.Fatal("invalid boost accepted", boosts, err)
		}
	}
	first = save(first.ID, "Renamed", []string{account.ID}, map[string]int64{account.ID: 0})
	if first.PreferenceBoosts[account.ID] != 0 {
		t.Fatal("reset failed", first)
	}
	save(first.ID, "Renamed", []string{account.ID}, map[string]int64{account.ID: 100})
	save(first.ID, "Renamed", []string{}, nil)
	first = save(first.ID, "Renamed", []string{account.ID}, nil)
	if first.PreferenceBoosts[account.ID] != 0 {
		t.Fatal("readded membership inherited old boost", first)
	}
	if _, err := connection.Exec("CREATE TRIGGER fail_boost_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END"); err != nil {
		t.Fatal(err)
	}
	ctx = audit.WithActor(ctx, audit.Actor{ID: 1, TenantID: 1, Username: "synthetic-admin", Role: "admin", Source: "user"})
	if _, err := service.Save(ctx, first.ID, groups.Input{Name: "Atomic", Enabled: true, AccountIDs: []string{account.ID}, PreferenceBoosts: map[string]int64{account.ID: 200}}); err == nil {
		t.Fatal("audit failure did not abort")
	}
	actual, err := service.Get(context.Background(), first.ID)
	if err != nil || actual.Name != "Renamed" || actual.PreferenceBoosts[account.ID] != 0 {
		t.Fatal("partial save", actual, err)
	}
}
