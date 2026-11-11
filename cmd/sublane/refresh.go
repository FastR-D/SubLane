package main

import (
	"context"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/logging"
	"github.com/murongg/SubLane/internal/storage/db"
)

func (r *tenantRegistry) maintainAccounts(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()
	cursor := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var err error
			cursor, err = r.refreshAccounts(ctx, cursor)
			if err != nil && ctx.Err() == nil {
				logging.From(ctx).ErrorContext(ctx, "Account refresh scan failed", "error_code", "refresh_scan_failed")
			}
		}
	}
}

// A bounded page per tick and one rotation at a time keep background traffic bounded.
// Retaining the cursor prevents failing early accounts from starving later workspaces.
func (r *tenantRegistry) refreshAccounts(ctx context.Context, after string) (string, error) {
	lead := r.refreshLead
	if lead == 0 {
		lead = accounts.CodexRefreshLead
	}
	rows, err := db.New(r.db).ListDueCodexRefresh(ctx, db.ListDueCodexRefreshParams{BeforeExpiry: r.clock().Add(lead).Unix(), AfterID: after})
	if err != nil {
		return after, err
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return after, err
		}
		runtime := r.acquire(row.TenantID)
		if runtime == nil {
			return after, context.Canceled
		}
		// The runtime reference prevents eviction until this owner has saved the rotation.
		_ = runtime.accounts.RefreshDue(ctx, row.ID, r.provider.Refresh)
		r.release(runtime)
		after = row.ID
	}
	if len(rows) < 100 {
		after = ""
	}
	return after, nil
}
