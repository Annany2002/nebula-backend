package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/mattn/go-sqlite3"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

const backupColumns = "backup_id, db_name, status, size_bytes, sha256, created_at"

func ensureBackupSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS database_backups (
		backup_id TEXT PRIMARY KEY NOT NULL,
		owner_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE RESTRICT,
		source_database_id INTEGER NOT NULL,
		db_name TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('creating','ready','deleting','deleted')),
		size_bytes INTEGER NOT NULL DEFAULT 0 CHECK(size_bytes >= 0),
		sha256 TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	CREATE INDEX IF NOT EXISTS idx_backups_owner_created ON database_backups(owner_id, created_at DESC, backup_id);
	CREATE INDEX IF NOT EXISTS idx_backups_owner_database ON database_backups(owner_id, db_name, created_at DESC, backup_id);`)
	return err
}

type backupScanner interface {
	Scan(...any) error
}

func scanBackup(row backupScanner) (domain.DatabaseBackup, error) {
	var backup domain.DatabaseBackup
	err := row.Scan(&backup.BackupID, &backup.DBName, &backup.Status, &backup.SizeBytes, &backup.SHA256, &backup.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrBackupNotFound
	}
	return backup, err
}

// Get returns metadata without accessing or disclosing the snapshot's path.
func (s *BackupStore) Get(ctx context.Context, owner, id string) (domain.DatabaseBackup, error) {
	if err := validateBackupIdentity(owner, id); err != nil {
		return domain.DatabaseBackup{}, err
	}
	return scanBackup(s.meta.QueryRowContext(ctx, "SELECT "+backupColumns+" FROM database_backups WHERE owner_id=? AND backup_id=? AND status!='deleted'", owner, id))
}

// List returns a stable, bounded page, including backups whose source was deleted.
func (s *BackupStore) List(ctx context.Context, owner, dbName string, limit, offset int) ([]domain.DatabaseBackup, int, error) {
	if !validBackupOwner(owner) || (dbName != "" && !validBackupDatabaseName(dbName)) || limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, 0, ErrInvalidBackupRequest
	}
	where := "owner_id=? AND status!='deleted'"
	args := []any{owner}
	if dbName != "" {
		where += " AND db_name=?"
		args = append(args, dbName)
	}
	tx, err := s.meta.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback() //nolint:errcheck // A read-only transaction has no writes to recover.
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM database_backups WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	//nolint:gosec // Columns and conditions are internal constants; all client values are bound parameters.
	rows, err := tx.QueryContext(ctx, "SELECT "+backupColumns+" FROM database_backups WHERE "+where+" ORDER BY created_at DESC, backup_id ASC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err below reports iteration errors.
	backups := make([]domain.DatabaseBackup, 0)
	for rows.Next() {
		backup, err := scanBackup(rows)
		if err != nil {
			return nil, 0, err
		}
		backups = append(backups, backup)
	}
	return backups, total, rows.Err()
}

// reserve opens the original source while holding a short metadata write lock.
// A concurrent source deletion cannot replace the name before this connection is pinned.
func (s *BackupStore) reserve(ctx context.Context, owner, name, id string) (source *sql.DB, resultErr error) {
	tx, err := s.meta.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // Committed reservations are cleaned by Create on failure.
	result, err := tx.ExecContext(ctx, `INSERT INTO database_backups(backup_id,owner_id,source_database_id,db_name,status)
		SELECT ?,owner_id,database_id,db_name,'creating' FROM databases WHERE owner_id=? AND db_name=?
		AND (SELECT count(*) FROM database_backups WHERE owner_id=? AND status!='deleted') < ?
		AND (SELECT coalesce(sum(CASE WHEN status='creating' THEN ? ELSE size_bytes END),0) FROM database_backups WHERE owner_id=? AND status!='deleted') <= ?`,
		id, owner, name, owner, s.maxCount, MaxSQLiteImportBytes, owner, s.maxBytes-MaxSQLiteImportBytes)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
			return nil, ErrBackupConflict
		}
		return nil, fmt.Errorf("reserve backup: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	var path string
	if err := tx.QueryRowContext(ctx, "SELECT file_path FROM databases WHERE owner_id=? AND db_name=?", owner, name).Scan(&path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDatabaseNotFound
		}
		return nil, err
	}
	if count == 0 {
		return nil, ErrBackupQuota
	}
	if err := requireRegularBackupFile(path); err != nil {
		return nil, fmt.Errorf("open backup source: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absPath, RawQuery: "mode=ro&_busy_timeout=5000"}).String()
	source, err = sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	opened := source
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, opened.Close())
		}
	}()
	source.SetMaxOpenConns(1)
	source.SetMaxIdleConns(1)
	if err := source.PingContext(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return source, nil
}

// Recover removes interrupted creations/deletions before accepting requests.
// This filesystem-backed store requires one server process per metadata directory.
func (s *BackupStore) Recover(ctx context.Context) error {
	for {
		var owner, id string
		err := s.meta.QueryRowContext(ctx, "SELECT owner_id,backup_id FROM database_backups WHERE status IN ('creating','deleting') ORDER BY backup_id LIMIT 1").Scan(&owner, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.cleanup(ctx, owner, id); err != nil {
			return fmt.Errorf("recover backup %s: %w", id, err)
		}
	}
}

func removeBackupFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
