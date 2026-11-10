package gateway

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/murongg/SubLane/internal/allocations"
	"github.com/murongg/SubLane/internal/storage/db"
)

const maintenanceBatchSize = 500

// MaintainHistory runs one catch-up pass on startup and repeats it independently of requests.
func MaintainHistory(ctx context.Context, connection *sql.DB) {
	run := func() {
		if err := pruneHistory(ctx, connection, time.Now()); err != nil && ctx.Err() == nil {
			slog.Error("Unable to prune retained history", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func pruneHistory(ctx context.Context, connection *sql.DB, now time.Time) error {
	q := db.New(connection)
	today := now.UTC().Truncate(24 * time.Hour).Unix()
	prunes := []func(context.Context) (int64, error){
		func(ctx context.Context) (int64, error) {
			return q.PruneSchedulingSamples(ctx, db.PruneSchedulingSamplesParams{LongBefore: now.Add(-90 * 24 * time.Hour).Unix(), ShortBefore: now.Add(-7 * 24 * time.Hour).Unix()})
		},
		func(ctx context.Context) (int64, error) {
			return q.PruneSchedulingUsage(ctx, now.Add(-90*24*time.Hour).Unix())
		},
		func(ctx context.Context) (int64, error) {
			return allocations.Prune(ctx, q, now.Add(-90*24*time.Hour).Unix())
		},
		func(ctx context.Context) (int64, error) { return q.PruneStatistics(ctx, today-89*86400) },
		func(ctx context.Context) (int64, error) { return q.PruneHourlyUsage(ctx, today-90*86400) },
		// Local 90-day reports can begin on the preceding UTC date.
		func(ctx context.Context) (int64, error) { return q.PruneMinuteStatistics(ctx, today-90*86400) },
		func(ctx context.Context) (int64, error) { return q.PruneRequests(ctx, now.Add(-7*24*time.Hour).Unix()) },
	}
	for _, prune := range prunes {
		for {
			deleted, err := prune(ctx)
			if err != nil {
				return err
			}
			if deleted < maintenanceBatchSize {
				break
			}
			// SQLite has one writer; give request settlement a chance between large catch-up batches.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	return nil
}
