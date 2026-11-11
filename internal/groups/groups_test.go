package groups_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
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

func TestDeleteRetiresGroupOnlyAfterKeysAndAllowancesAreGone(t *testing.T) {
	connection, service, member, account := fixture(t)
	ctx := context.Background()
	group, err := service.Save(ctx, 0, groups.Input{Name: "Retired", Enabled: true, AccountIDs: []string{account.ID}, Routing: &groups.Routing{Preference: "protocol", SessionSticky: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !group.Routing.SessionSticky {
		t.Fatalf("session stickiness was not saved: %+v", group.Routing)
	}
	if err := service.SetMemberGroups(ctx, member.ID, []int64{group.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(`INSERT INTO api_keys(id,user_id,group_id,name,prefix,token_hash,created_at) VALUES(41,?,?,'synthetic','sk-syn',randomblob(32),1)`, member.ID, group.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, group.ID); !errors.Is(err, groups.ErrInUse) {
		t.Fatalf("active key did not block deletion: %v", err)
	}
	if _, err := connection.Exec(`INSERT INTO allocation_schemes(id,name,group_id,enabled,created_at) VALUES(7,'Synthetic',?,1,1)`, group.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, group.ID); !errors.Is(err, groups.ErrAllocated) {
		t.Fatalf("allowance did not block deletion: %v", err)
	}
	if _, err := connection.Exec(`DELETE FROM allocation_schemes WHERE id=7; UPDATE api_keys SET revoked_at=2 WHERE id=41`); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, group.ID); !errors.Is(err, groups.ErrNotFound) {
		t.Fatalf("deleted group remains readable: %v", err)
	}
	if list, err := service.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("deleted group remains listed: %+v, %v", list, err)
	}
	if choices, err := service.Available(ctx, member.ID); err != nil || len(choices) != 0 {
		t.Fatalf("deleted group remains available: %+v, %v", choices, err)
	}
	// The row and revoked key survive so history and usage aggregates keep their workspace scope.
	var retained, members, resources int
	if err := connection.QueryRow(`SELECT (SELECT count(*) FROM account_groups WHERE id=? AND deleted_at IS NOT NULL AND enabled=0 AND name='Retired'),
		(SELECT count(*) FROM group_members WHERE group_id=?), (SELECT count(*) FROM group_accounts WHERE group_id=?)`, group.ID, group.ID, group.ID).Scan(&retained, &members, &resources); err != nil {
		t.Fatal(err)
	}
	if retained != 1 || members != 0 || resources != 0 {
		t.Fatalf("retired=%d members=%d resources=%d", retained, members, resources)
	}
	if _, err := service.Save(ctx, 0, groups.Input{Name: "Retired", Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatalf("retired name was not released: %v", err)
	}
	if err := service.Delete(ctx, group.ID); !errors.Is(err, groups.ErrNotFound) {
		t.Fatalf("repeated deletion: %v", err)
	}
}

func TestDeletedGroupNamesNeverCollide(t *testing.T) {
	_, service, _, _ := fixture(t)
	ctx := context.Background()
	group, err := service.Save(ctx, 0, groups.Input{Name: "Synthetic group", Enabled: true, AccountIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	// Any label is valid, including one shaped like a retired-name suffix.
	if _, err := service.Save(ctx, 0, groups.Input{Name: fmt.Sprintf("Synthetic group (#%d)", group.ID), Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, group.ID); err != nil {
		t.Fatal("deletion collided with another group's name", err)
	}
	reused, err := service.Save(ctx, 0, groups.Input{Name: "synthetic GROUP", Enabled: true, AccountIDs: []string{}})
	if err != nil {
		t.Fatal("released name could not be reused", err)
	}
	if err := service.Delete(ctx, reused.ID); err != nil {
		t.Fatal("a second retired group with the same name was rejected", err)
	}
	if _, err := service.Save(ctx, 0, groups.Input{Name: "Synthetic group", Enabled: true, AccountIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, 0, groups.Input{Name: "SYNTHETIC group", Enabled: true, AccountIDs: []string{}}); !errors.Is(err, groups.ErrDuplicate) {
		t.Fatal("active names are no longer unique", err)
	}
}
