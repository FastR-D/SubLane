package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestBranchSnapshotHistoryAcceptsPublishedPrefixes(t *testing.T) {
	branches := map[string][]string{
		"main":   {"030_group_lifecycle.sql", "031_image_requests.sql", "032_affinity_preferences.sql", "033_group_active_names.sql", "034_allocation_image_input.sql"},
		"deploy": {"030_group_preference_boosts.sql", "031_scheduling_policy.sql", "032_scheduling_history.sql", "033_scheduling_cycles.sql"},
	}
	for branch, names := range branches {
		t.Run(branch, func(t *testing.T) {
			ctx := t.Context()
			conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "synthetic-history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetMaxOpenConns(1)
			if _, err := conn.Exec(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			entries, err := migrations.ReadDir("migrations")
			if err != nil {
				t.Fatal(err)
			}
			version := 0
			for _, entry := range entries {
				if entry.Name() >= "030_" {
					break
				}
				if err := apply(ctx, conn, entry.Name()); err != nil {
					t.Fatal(err)
				}
				version++
			}
			for _, name := range names {
				if err := apply(ctx, conn, name); err != nil {
					t.Fatal(err)
				}
				version++
				if got, err := ValidateSnapshot(ctx, conn); err != nil || got != version {
					t.Fatalf("%s snapshot: version=%d want=%d err=%v", name, got, version, err)
				}
			}
		})
	}
}

func TestBranchSnapshotHistoryRejectsGapsAndMixedHistories(t *testing.T) {
	for _, missing := range []string{"029_usage_minutes.sql", "030_group_lifecycle.sql", "031_scheduling_policy.sql", "032_affinity_preferences.sql"} {
		t.Run(missing, func(t *testing.T) {
			conn, err := Open(t.Context(), filepath.Join(t.TempDir(), "synthetic-gap.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.Exec(`DELETE FROM schema_migrations WHERE name=?`, missing); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateSnapshot(t.Context(), conn); err == nil {
				t.Fatalf("accepted incomplete history missing %s", missing)
			}
		})
	}
}
