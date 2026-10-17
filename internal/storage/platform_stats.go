package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

// GetPlatformStats snapshots registry counts, then inspects tenant schemas with bounded read-only connections.
// Tenant schemas can change independently; an unavailable file makes the table total unknown, never zero.
func GetPlatformStats(ctx context.Context, db *sql.DB, directory string) (domain.PlatformStats, error) {
	stats, paths, err := platformRegistry(ctx, db)
	if err != nil {
		return domain.PlatformStats{}, err
	}
	var tables int64
	for _, path := range paths {
		count, err := countTenantTables(ctx, directory, path)
		if ctx.Err() != nil {
			return domain.PlatformStats{}, ctx.Err()
		}
		if err != nil {
			stats.DatabasesUnavailable++
			continue
		}
		stats.DatabasesCounted++
		tables += count
	}
	if stats.DatabasesUnavailable == 0 {
		stats.TotalTables = &tables
	}
	return stats, nil
}

func platformRegistry(ctx context.Context, db *sql.DB) (domain.PlatformStats, []string, error) {
	var stats domain.PlatformStats
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return stats, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&stats.TotalUsers); err != nil {
		return stats, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT file_path FROM databases ORDER BY database_id`)
	if err != nil {
		return stats, nil, err
	}
	defer rows.Close()
	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return stats, nil, err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return stats, nil, err
	}
	if err := rows.Close(); err != nil {
		return stats, nil, err
	}
	stats.TotalDatabases = int64(len(paths))
	return stats, paths, tx.Commit()
}

func countTenantTables(parent context.Context, directory, path string) (int64, error) {
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	absolute, err := registeredStatsPath(directory, path)
	if err != nil {
		return 0, err
	}
	// Never use immutable=1 on live WAL databases: committed schema changes may be in the WAL.
	uri := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro&_query_only=on&_busy_timeout=100"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA trusted_schema=OFF`); err != nil {
		return 0, err
	}
	var count int64
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_list
 WHERE schema='main' AND type IN ('table','virtual')
 AND lower(substr(name,1,7))<>'sqlite_' AND lower(substr(name,1,8))<>'_nebula_'`).Scan(&count)
	return count, err
}

func registeredStatsPath(directory, path string) (string, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("registered database is outside the storage directory")
	}
	// Tenant APIs cannot modify registered paths or the server filesystem. Reject
	// unexpected symlinks/non-files rather than following operator-created indirection.
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("registered database path contains a symlink")
		}
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("registered database must be a regular file")
	}
	return absolute, nil
}
