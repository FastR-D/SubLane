package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func OpenReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("backup source must be a regular database file")
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	query := u.Query()
	query.Set("mode", "ro")

	u.RawQuery = query.Encode()
	connection, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	connection.SetMaxOpenConns(1)
	connection.SetMaxIdleConns(1)
	if err := LimitSnapshotValues(ctx, connection); err != nil {
		connection.Close()
		return nil, err
	}
	if _, err := connection.ExecContext(ctx, "PRAGMA cache_size=-2048; PRAGMA busy_timeout=1000; PRAGMA trusted_schema=OFF"); err != nil {
		connection.Close()
		return nil, err
	}
	if err := connection.PingContext(ctx); err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

// LimitSnapshotValues bounds allocations when querying an imported database, independently of archive size.
func LimitSnapshotValues(ctx context.Context, connection *sql.DB) error {
	conn, err := connection.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, limit := range []int{sqlite3.SQLITE_LIMIT_LENGTH, sqlite3.SQLITE_LIMIT_SQL_LENGTH} {
		if _, err := sqlite.Limit(conn, limit, 1<<20); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot owns a new destination in a private directory; it never copies a live WAL file directly.
func Snapshot(ctx context.Context, source, target string, maxBytes int64) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if maxBytes <= 0 {
		return errors.New("invalid backup database size limit")
	}
	connection, err := OpenReadOnly(ctx, source)
	if err != nil {
		return err
	}
	defer connection.Close()
	var pageSize, pageCount int64
	if err := connection.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return err
	}
	if pageSize <= 0 || pageCount > maxBytes/pageSize {
		return errors.New("backup database exceeds size limit")
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); result == nil {
			result = err
		}
		if result != nil {
			_ = os.Remove(target)
		}
	}()
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=rw"}
	conn, err := connection.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = conn.Raw(func(raw any) (result error) {
		driver, ok := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("SQLite backup API unavailable")
		}
		job, err := driver.NewBackup(uri.String())
		if err != nil {
			return err
		}
		defer func() {
			if err := job.Finish(); result == nil {
				result = err
			}
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := job.Step(128)
			if int64(job.PageCount()) > maxBytes/pageSize {
				return errors.New("backup database exceeds size limit")
			}
			if err != nil {
				var sqliteErr *sqlite.Error
				if !errors.As(err, &sqliteErr) || (sqliteErr.Code()&255 != sqlite3.SQLITE_BUSY && sqliteErr.Code()&255 != sqlite3.SQLITE_LOCKED) {
					return err
				}
				timer := time.NewTimer(25 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			if !more {
				return nil
			}
		}
	})
	if err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	return file.Sync()
}

func CurrentSchemaVersion() (int, error) {
	entries, err := migrations.ReadDir("migrations")
	return len(entries), err
}

// ValidateSnapshot checks the original migration history before any restore can apply newer migrations.
func ValidateSnapshot(ctx context.Context, connection *sql.DB) (int, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	rows, err := connection.QueryContext(ctx, "SELECT name FROM schema_migrations ORDER BY name")
	if err != nil {
		return 0, err
	}
	history := make([]string, 0, len(entries))
	formerProxyHistory := false
	// Accept only a prefix of a published history, including the two branches before their merge.
	current := make([]string, 0, len(entries))
	common := []string{}
	for _, entry := range entries {
		current = append(current, entry.Name())
		if entry.Name() < "030_" {
			common = append(common, entry.Name())
		}
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, err
		}
		// The retired proxy migration still counts in archive metadata.
		if len(history) >= 3 && !formerProxyHistory && name == formerProxyChecksMigration {
			formerProxyHistory = true
			continue
		}
		if len(history) >= len(entries) {
			rows.Close()
			return 0, errors.New("backup has an unknown or incomplete migration history")
		}
		history = append(history, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(history) == 0 {
		return 0, errors.New("backup has no SubLane schema")
	}
	valid := func(expected []string) bool {
		return len(history) <= len(expected) && slices.Equal(history, expected[:len(history)])
	}
	mainHistory := append(slices.Clone(common), "030_group_lifecycle.sql", "031_image_requests.sql", "032_affinity_preferences.sql", "033_group_active_names.sql", "034_allocation_image_input.sql")
	deployHistory := append(slices.Clone(common), "030_group_preference_boosts.sql", "031_scheduling_policy.sql", "032_scheduling_history.sql", "033_scheduling_cycles.sql")
	if !valid(current) && !valid(mainHistory) && !valid(deployHistory) {
		return 0, errors.New("backup has an unknown or incomplete migration history")
	}
	version := len(history)
	if formerProxyHistory {
		version++
		if err := verifyProxyCheckColumns(ctx, connection); err != nil {
			return 0, errors.New("backup has an incomplete proxy check schema")
		}
	}
	var integrity string
	if err := connection.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity); err != nil {
		return 0, err
	}
	if integrity != "ok" {
		return 0, errors.New("backup database integrity check failed")
	}
	foreign, err := connection.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return 0, err
	}
	defer foreign.Close()
	if foreign.Next() {
		return 0, errors.New("backup database has invalid references")
	}
	return version, foreign.Err()
}
