package storage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-sqlite3"

	"github.com/Annany2002/nebula-backend/internal/core"
)

// MaxSQLiteImportBytes bounds the standalone snapshot accepted by the import API.
const MaxSQLiteImportBytes int64 = 64 << 20

// ErrInvalidSnapshot means an upload cannot be safely imported as a standalone SQLite database.
var ErrInvalidSnapshot = errors.New("invalid SQLite snapshot")

func init() {
	sql.Register("nebula_sqlite_import", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			conn.SetLimit(sqlite3.SQLITE_LIMIT_LENGTH, int(MaxSQLiteImportBytes))
			conn.SetLimit(sqlite3.SQLITE_LIMIT_SQL_LENGTH, 1<<20)
			conn.SetLimit(sqlite3.SQLITE_LIMIT_EXPR_DEPTH, 100)
			conn.SetLimit(sqlite3.SQLITE_LIMIT_VDBE_OP, 250000)
			conn.SetLimit(sqlite3.SQLITE_LIMIT_ATTACHED, 0)
			// This connection only inspects a private, immutable upload. No schema actions run.
			_, err := conn.Exec("PRAGMA trusted_schema=OFF; PRAGMA query_only=ON; PRAGMA cell_size_check=ON; PRAGMA mmap_size=0; PRAGMA cache_size=-2048;", nil)
			return err
		},
	})
}

// ImportSQLiteDatabase validates a private snapshot, then publishes a new database.
// The caller owns snapshotPath and must remove it after this call. Existing files,
// sidecars and registrations are never replaced. No account or API-key metadata is imported.
func ImportSQLiteDatabase(ctx context.Context, metaDB *sql.DB, storageDir, owner, name, snapshotPath string) (int64, error) {
	if !core.IsValidIdentifier(name) || owner == "" || owner == "." || owner == ".." || filepath.Base(owner) != owner {
		return 0, fmt.Errorf("%w: invalid destination", ErrInvalidSnapshot)
	}
	size, err := validateSQLiteSnapshot(ctx, snapshotPath)
	if err != nil {
		return 0, err
	}
	userDir := filepath.Join(storageDir, owner)
	if err := os.MkdirAll(userDir, 0o750); err != nil {
		return 0, fmt.Errorf("prepare import directory: %w", err)
	}
	dirInfo, err := os.Lstat(userDir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("import directory must be a real directory")
	}
	if err := syncImportDirectory(storageDir); err != nil {
		return 0, err
	}
	destination := filepath.Join(userDir, name+".db")

	// Reserve the owner/name in a metadata transaction. Other registrations cannot
	// win this name while the file is published; readers see only the committed import.
	tx, err := metaDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // A committed transaction has nothing to roll back.
	_, err = tx.ExecContext(ctx, "INSERT INTO databases(owner_id, db_name, file_path) VALUES (?, ?, ?)", owner, name, destination)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return 0, ErrDatabaseExists
		}
		return 0, fmt.Errorf("register import: %w", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(destination + suffix); err == nil {
			return 0, ErrDatabaseExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("inspect import destination: %w", err)
		}
	}
	// A hard link atomically publishes the complete staged file without overwriting
	// an existing destination. Staging must be on the same filesystem as storageDir.
	if err := os.Link(snapshotPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, ErrDatabaseExists
		}
		return 0, fmt.Errorf("publish import: %w", err)
	}
	// Persist the directory entry before committing metadata that points to it.
	if err := syncImportDirectory(userDir); err != nil {
		return 0, errors.Join(err, os.Remove(destination))
	}
	if err := tx.Commit(); err != nil {
		if removeErr := os.Remove(destination); removeErr != nil {
			return 0, errors.Join(err, fmt.Errorf("clean up failed import: %w", removeErr))
		}
		return 0, fmt.Errorf("commit import: %w", err)
	}
	return size, nil
}

func validateSQLiteSnapshot(ctx context.Context, path string) (int64, error) {
	size, err := validateSnapshotHeader(path)
	if err != nil {
		return 0, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absPath, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := sql.Open("nebula_sqlite_import", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close() //nolint:errcheck // Read-only validation connection.
	db.SetMaxOpenConns(1)
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity); err != nil {
		return 0, snapshotValidationError(ctx, err)
	}
	if integrity != "ok" {
		return 0, fmt.Errorf("%w: integrity check failed", ErrInvalidSnapshot)
	}
	// Virtual-table modules and their shadow schemas need a separate compatibility
	// policy. This first import contract accepts ordinary tables and dependent objects.
	var unsupported int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_list() WHERE schema='main' AND type IN ('virtual','shadow')`).Scan(&unsupported); err != nil {
		return 0, snapshotValidationError(ctx, err)
	}
	if unsupported != 0 {
		return 0, fmt.Errorf("%w: virtual and shadow tables are not supported", ErrInvalidSnapshot)
	}
	if err := validateSnapshotInternalSchema(ctx, db); err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return 0, snapshotValidationError(ctx, err)
	}
	defer rows.Close() //nolint:errcheck
	if rows.Next() {
		return 0, fmt.Errorf("%w: foreign key violations", ErrInvalidSnapshot)
	}
	if err := rows.Err(); err != nil {
		return 0, snapshotValidationError(ctx, err)
	}
	return size, nil
}

// Native Nebula exports contain this timestamp table. Accept only the exact
// platform definition and its implicit index, never arbitrary reserved objects
// or triggers/indexes that run against internal metadata during normal listing.
func validateSnapshotInternalSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema
 WHERE lower(substr(name,1,8))='_nebula_' OR lower(substr(tbl_name,1,8))='_nebula_'`)
	if err != nil {
		return snapshotValidationError(ctx, err)
	}
	defer rows.Close() //nolint:errcheck // Read-only schema inspection.
	canonical := strings.TrimSuffix(strings.Join(strings.Fields(strings.Replace(tableMetadataDDL, " IF NOT EXISTS", "", 1)), " "), ";")
	for rows.Next() {
		var kind, name, table, definition string
		if err := rows.Scan(&kind, &name, &table, &definition); err != nil {
			return snapshotValidationError(ctx, err)
		}
		if table == "_nebula_table_metadata" && kind == "table" && name == table && strings.Join(strings.Fields(definition), " ") == canonical {
			continue
		}
		if table == "_nebula_table_metadata" && kind == "index" && name == "sqlite_autoindex__nebula_table_metadata_1" && definition == "" {
			continue
		}
		return fmt.Errorf("%w: reserved or modified internal schema objects are not supported", ErrInvalidSnapshot)
	}
	if err := rows.Err(); err != nil {
		return snapshotValidationError(ctx, err)
	}
	return nil
}

func snapshotValidationError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrCantOpen || sqliteErr.Code == sqlite3.ErrIoErr || sqliteErr.Code == sqlite3.ErrNomem) {
		return fmt.Errorf("inspect snapshot: %w", err)
	}
	return fmt.Errorf("%w: unreadable or unsupported schema", ErrInvalidSnapshot)
}

func validateSnapshotHeader(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close() //nolint:errcheck // Only reading the private staged file.
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 512 || info.Size() > MaxSQLiteImportBytes {
		return 0, fmt.Errorf("%w: invalid snapshot size", ErrInvalidSnapshot)
	}
	var header [100]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || string(header[:16]) != "SQLite format 3\x00" {
		return 0, fmt.Errorf("%w: not a SQLite database", ErrInvalidSnapshot)
	}
	pageSize := int64(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 || info.Size()%pageSize != 0 {
		return 0, fmt.Errorf("%w: invalid page size or truncated snapshot", ErrInvalidSnapshot)
	}
	pages := int64(binary.BigEndian.Uint32(header[28:32]))
	if pages != 0 && binary.BigEndian.Uint32(header[24:28]) == binary.BigEndian.Uint32(header[92:96]) && pages*pageSize != info.Size() {
		return 0, fmt.Errorf("%w: snapshot page count does not match its size", ErrInvalidSnapshot)
	}
	return info.Size(), nil
}

func syncImportDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close() //nolint:errcheck // Directory descriptor used only for synchronization.
	return dir.Sync()
}
