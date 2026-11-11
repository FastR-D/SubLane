package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	q := u.Query()
	for _, pragma := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)"} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	// Serialize SQLite writes; no request may hold a transaction during upstream IO.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	// The consolidated initialization SQL cannot run over the former pre-release schema.
	var legacy bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='001_settings.sql')`).Scan(&legacy); err != nil {
		return err
	}
	if legacy {
		return fmt.Errorf("database uses an older pre-release schema; preserve its data directory and start this version with a fresh one")
	}
	if err := reconcileProxyMigrations(ctx, db); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := apply(ctx, db, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, name string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// SQLite requires disabling FK actions outside the transaction for a table rebuild.
	// The transaction's foreign_key_check still gates publication; restore enforcement before reuse.
	if name == "011_grok.sql" || name == "015_api_upstreams.sql" || name == "026_catalog_capabilities.sql" || name == "027_allocation_anchors.sql" || name == "033_group_active_names.sql" {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
		defer conn.ExecContext(context.Background(), "PRAGMA legacy_alter_table=OFF")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE name = ?", name).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	data, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(data)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(name) VALUES (?)", name); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	checkErr := rows.Err()
	rows.Close()
	if checkErr != nil {
		return checkErr
	}
	if invalid {
		return fmt.Errorf("migration %s violates foreign keys", name)
	}
	// Record the migration in the same transaction so a crash never marks partial work complete.
	return tx.Commit()
}
