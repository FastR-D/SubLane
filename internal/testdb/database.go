// Package testdb creates isolated, fully migrated SQLite fixtures for domain and HTTP tests.
// Migration and upgrade tests must continue to use storage.Open directly.
package testdb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"

	"github.com/murongg/SubLane/internal/storage"
)

var template = sync.OnceValues(func() ([]byte, error) {
	directory, err := os.MkdirTemp("", "sublane-testdb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "template.db")
	conn, err := storage.Open(context.Background(), path)
	if err != nil {
		return nil, err
	}
	// Closing the only connection checkpoints WAL before any bytes are copied.
	// Only immutable bytes survive initialization; fixtures share no SQLite files or connections.
	if err := conn.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
})

// Open copies the process-local empty schema to a new path, then uses production connection setup.
// It never replaces an existing database; reopen fixtures with storage.Open to retain their data.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := template()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, closeErr
	}
	return storage.Open(ctx, path)
}
