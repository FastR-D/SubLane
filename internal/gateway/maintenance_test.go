package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestHistoryReadsDoNotDeleteAndMaintenancePrunesInBatches(t *testing.T) {
	ctx := context.Background()
	connection, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := connection.ExecContext(ctx, "INSERT INTO users(id,username,role,password_hash,enabled,created_at) VALUES(1,'synthetic-owner','admin','synthetic-hash',1,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(ctx, "INSERT INTO tenants(id,name,owner_user_id,created_at) VALUES(1,'Synthetic workspace',1,1)"); err != nil {
		t.Fatal(err)
	}
	oldDay := now.Add(-91 * 24 * time.Hour).Truncate(24 * time.Hour).Unix()
	if _, err := connection.ExecContext(ctx, "INSERT INTO usage_daily(day,user_id,group_id,provider,model) VALUES(?,1,1,'codex','synthetic-model')", oldDay); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(ctx, "INSERT INTO usage_hourly(tenant_id,hour,user_id,requests,input_tokens,output_tokens,input_reported,output_reported) VALUES(1,?,1,1,0,0,0,0)", oldDay); err != nil {
		t.Fatal(err)
	}
	for _, minute := range []int64{oldDay, now.UTC().Truncate(24*time.Hour).Unix() - 90*86400 + 3600} {
		if _, err := connection.Exec("INSERT INTO usage_minutes(tenant_id,minute,user_id,group_id,provider,model,requests) VALUES(1,?,1,1,'codex','synthetic-model',1)", minute); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 1201; i++ {
		started := now.Add(-8 * 24 * time.Hour).Unix()
		if i == 1200 {
			started = now.Unix()
		}
		if _, err := connection.ExecContext(ctx, `INSERT INTO request_records(user_id,key_id,group_id,transport,operation,started_at,duration_ms,outcome)
			VALUES(1,1,1,'http','responses',?,1,'success')`, started); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{db: connection, queries: db.New(connection), tenantID: 1, now: func() time.Time { return now }}
	if _, err := service.Requests(ctx, RequestFilter{}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := connection.QueryRowContext(ctx, "SELECT count(*) FROM request_records").Scan(&count); err != nil || count != 1201 {
		t.Fatalf("read changed history: count=%d err=%v", count, err)
	}
	if _, err := service.Statistics(ctx, 7); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_daily", "usage_hourly"} {
		if err := connection.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("statistics read changed %s: count=%d err=%v", table, count, err)
		}
	}
	if err := pruneHistory(ctx, connection, now); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRowContext(ctx, "SELECT count(*) FROM request_records").Scan(&count); err != nil || count != 1 {
		t.Fatalf("maintenance retention: count=%d err=%v", count, err)
	}
	if err := connection.QueryRow("SELECT count(*) FROM usage_minutes").Scan(&count); err != nil || count != 1 {
		t.Fatal("maintenance lost the first local reporting day or retained expired minutes", count, err)
	}
	for _, table := range []string{"usage_daily", "usage_hourly"} {
		if err := connection.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("maintenance did not prune %s: count=%d err=%v", table, count, err)
		}
	}
}
