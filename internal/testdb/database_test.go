package testdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/murongg/SubLane/internal/storage"
)

func TestOpenKeepsParallelDatabasesIsolatedAndDurable(t *testing.T) {
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "nested", "synthetic # database.db")
			conn, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			want, err := storage.CurrentSchemaVersion()
			if err != nil {
				t.Fatal(err)
			}
			if got, err := storage.ValidateSnapshot(ctx, conn); err != nil || got != want {
				t.Fatalf("schema=%d want=%d err=%v", got, want, err)
			}
			var journal string
			var foreignKeys int
			if err := conn.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
				t.Fatalf("journal=%s err=%v", journal, err)
			}
			if err := conn.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
				t.Fatalf("foreign_keys=%d err=%v", foreignKeys, err)
			}
			var value string
			if err := conn.QueryRow("SELECT value FROM settings WHERE key='synthetic-isolation'").Scan(&value); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("another fixture changed this database", err)
			}
			if _, err := conn.Exec("INSERT INTO settings(key,value) VALUES('synthetic-isolation',?)", t.Name()); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			conn, err = storage.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.QueryRow("SELECT value FROM settings WHERE key='synthetic-isolation'").Scan(&value); err != nil || value != t.Name() {
				t.Fatal("fixture data did not survive reopening", value, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("database permissions", info, err)
			}
		})
	}
}

func TestOpenDoesNotReplaceExistingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(path, []byte("synthetic existing data"), 0600); err != nil {
		t.Fatal(err)
	}
	if conn, err := Open(t.Context(), path); err == nil {
		conn.Close()
		t.Fatal("existing file replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "synthetic existing data" {
		t.Fatal("existing file changed", err)
	}
}

func TestOpenHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(t.TempDir(), "cancelled.db")
	if conn, err := Open(ctx, path); !errors.Is(err, context.Canceled) {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("cancelled open", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled open created a database", err)
	}
}

func BenchmarkOpen(b *testing.B) {
	// Warm the shared template outside the copy measurements; each test process pays this once.
	warm, err := Open(b.Context(), filepath.Join(b.TempDir(), "warm.db"))
	if err != nil {
		b.Fatal(err)
	}
	warm.Close()
	for name, open := range map[string]func(context.Context, string) (*sql.DB, error){
		"full_migrations": storage.Open,
		"template_copy":   Open,
	} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				conn, err := open(b.Context(), filepath.Join(b.TempDir(), "benchmark.db"))
				if err != nil {
					b.Fatal(err)
				}
				if err := conn.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
