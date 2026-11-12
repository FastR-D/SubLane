package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestRequestHistoryDoesNotCrossWorkspaces(t *testing.T) {
	ctx := context.Background()
	connection, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-admin", "synthetic-password", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(`INSERT INTO users(id,username,role,password_hash,enabled,created_at)
		VALUES(3,'synthetic-owner','member','synthetic-hash',1,1)`); err != nil {
		t.Fatal(err)
	}
	workspaceService := tenants.New(connection)
	other, err := workspaceService.Create(ctx, 3, "Other workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspaceService.AddMember(ctx, 3, other.ID, member.ID, tenants.RoleMember); err != nil {
		t.Fatal(err)
	}
	firstPool, err := groups.New(connection).Save(ctx, 0, groups.Input{Name: "Pool", Enabled: true, AccountIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	otherPool, err := groups.NewForTenant(connection, other.ID).Save(ctx, 0, groups.Input{Name: "Pool", Enabled: true, AccountIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(`INSERT INTO accounts(id,tenant_id,provider,name,account_id,status,credential,created_at,updated_at)
		VALUES('11111111111111111111111111111111',1,'codex','First account','first-subject','ready',x'01',1,1),
		('22222222222222222222222222222222',2,'codex','Second account','second-subject','ready',x'01',1,1)`); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []int64{firstPool.ID, otherPool.ID} {
		if err := db.New(connection).RecordRequest(ctx, db.RecordRequestParams{UserID: member.ID, GroupID: pool,
			Transport: "http", Operation: "responses", Outcome: "success", StartedAt: time.Now().Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	first := New(ctx, connection, nil, nil)
	defer first.Close()
	second := NewForTenant(ctx, connection, nil, nil, other.ID)
	defer second.Close()
	if err := second.SetConcurrency(ctx, "11111111111111111111111111111111", 3); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("workspace changed another account's concurrency: %v", err)
	}
	if err := second.Resume(ctx, "11111111111111111111111111111111"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("workspace resumed another account: %v", err)
	}
	if err := first.SetMemberLimits(ctx, member.ID, 10, 1); err != nil {
		t.Fatal(err)
	}
	if err := second.SetMemberLimits(ctx, member.ID, 20, 2); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		service *Service
		rpm     int64
	}{{first, 10}, {second, 20}} {
		limit, err := check.service.MemberLimits(ctx, member.ID)
		if err != nil || limit.RequestsPerMinute != check.rpm {
			t.Fatalf("member policy crossed workspaces: %+v, %v", limit, err)
		}
	}
	if err := first.admitMember(ctx, member.ID); err != nil {
		t.Fatal(err)
	}
	secondLimit, err := second.MemberLimits(ctx, member.ID)
	if err != nil || secondLimit.RequestsThisMinute != 0 {
		t.Fatalf("member rate window crossed workspaces: %+v, %v", secondLimit, err)
	}
	if observation, err := first.begin(ctx, member.ID, otherPool.ID, Responses); err == nil {
		observation.finish("rejected", "test_cleanup", "", "")
		t.Fatal("gateway admitted a pool from another workspace")
	}
	for _, check := range []struct {
		service *Service
		poolID  int64
	}{{first, firstPool.ID}, {second, otherPool.ID}} {
		page, err := check.service.UserRequests(ctx, member.ID, RequestFilter{})
		if err != nil || len(page.Requests) != 1 || page.Total != 1 || page.Requests[0].GroupID != check.poolID {
			t.Fatalf("request history leaked across workspaces: %+v, %v", page, err)
		}
	}
	for _, pool := range []int64{firstPool.ID, otherPool.ID} {
		if err := recordStatistics(ctx, db.New(connection), db.RecordRequestParams{UserID: member.ID,
			GroupID: pool, Model: "synthetic-model", Transport: "http", Operation: "responses",
			Outcome: "success", StartedAt: time.Now().Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, service := range []*Service{first, second} {
		stats, err := service.UserStatistics(ctx, member.ID, 1)
		if err != nil || stats.Totals.Requests != 1 {
			t.Fatalf("usage totals leaked across workspaces: %+v, %v", stats.Totals, err)
		}
		var activity int64
		for _, cell := range stats.Activity.Cells {
			activity += cell.Requests
		}
		if activity != 1 {
			t.Fatalf("hourly activity leaked across workspaces: %d", activity)
		}
	}
}
