package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
)

// CreateEmptyDatabase publishes a valid SQLite file and its registration together.
// Reuse import publication so existing files, sidecars and names are never adopted
// or overwritten, and a reported success is immediately usable for backups.
func CreateEmptyDatabase(ctx context.Context, meta *sql.DB, directory, owner, name string) error {
	release, err := LockDatabaseLifecycle(ctx, owner, name)
	if err != nil {
		return err
	}
	defer release()
	stage, err := os.CreateTemp(directory, ".sqlite-create-*")
	if err != nil {
		return err
	}
	path := stage.Name()
	defer os.Remove(path) //nolint:errcheck // Private temporary file, never a published database.
	if err := stage.Close(); err != nil {
		return err
	}
	empty, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	_, initErr := empty.ExecContext(ctx, "PRAGMA user_version=0")
	if err := errors.Join(initErr, empty.Close()); err != nil {
		return err
	}
	_, err = ImportSQLiteDatabase(ctx, meta, directory, owner, name, path)
	return err
}
