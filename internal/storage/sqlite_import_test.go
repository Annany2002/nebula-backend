package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteImportRollsBackFileWhenMetadataCommitFails(t *testing.T) {
	dir := t.TempDir()
	meta, err := sql.Open("sqlite3", filepath.Join(dir, "meta.db")+"?_foreign_keys=on")
	require.NoError(t, err)
	defer meta.Close()
	_, err = meta.Exec(`CREATE TABLE users(id TEXT PRIMARY KEY);
CREATE TABLE databases(owner_id TEXT REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED, db_name TEXT, file_path TEXT UNIQUE, UNIQUE(owner_id, db_name));`)
	require.NoError(t, err)
	sourcePath := filepath.Join(dir, "snapshot.db")
	source, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	_, err = source.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY); INSERT INTO items VALUES(1)")
	require.NoError(t, err)
	require.NoError(t, source.Close())
	_, err = ImportSQLiteDatabase(t.Context(), meta, dir, "missing-owner", "restored", sourcePath)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDatabaseExists)
	_, err = os.Stat(filepath.Join(dir, "missing-owner", "restored.db"))
	require.ErrorIs(t, err, os.ErrNotExist)
	var count int
	require.NoError(t, meta.QueryRow("SELECT count(*) FROM databases").Scan(&count))
	require.Zero(t, count)
	_, err = os.Stat(sourcePath)
	require.NoError(t, err, "caller-owned stage must remain intact")
}

func TestSQLiteImportRejectsSymlinkedOwnerDirectory(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "snapshot.db")
	source, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	_, err = source.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, source.Close())
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "owner")))
	_, err = ImportSQLiteDatabase(t.Context(), nil, dir, "owner", "target", sourcePath)
	require.ErrorContains(t, err, "real directory")
	files, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestSQLiteImportCancelledValidationDoesNotPublish(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "snapshot.db")
	source, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	_, err = source.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, source.Close())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ImportSQLiteDatabase(ctx, nil, dir, "owner", "target", sourcePath)
	require.ErrorIs(t, err, context.Canceled)
	_, err = os.Lstat(filepath.Join(dir, "owner"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
