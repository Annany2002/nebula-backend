package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mattn/go-sqlite3"
)

// CreateDatabaseSnapshot includes committed WAL pages without copying a live file.
func CreateDatabaseSnapshot(ctx context.Context, source *sql.DB, destination string) error {
	return createDatabaseSnapshot(ctx, source, destination, 0)
}

func createDatabaseSnapshot(ctx context.Context, source *sql.DB, destination string, maxBytes int64) error {
	var pageSize, pages int64
	if maxBytes > 0 {
		if err := source.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
			return err
		}
		if err := source.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
			return err
		}
		if pageSize*pages > maxBytes {
			return ErrBackupTooLarge
		}
	}
	target, err := sql.Open("sqlite3", destination)
	if err != nil {
		return err
	}
	defer target.Close() //nolint:errcheck // Connections and backup handles are closed below.
	src, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck
	dst, err := target.Conn(ctx)
	if err != nil {
		return err
	}
	defer dst.Close() //nolint:errcheck
	err = src.Raw(func(srcDriver any) error {
		return dst.Raw(func(dstDriver any) (resultErr error) {
			srcSQLite, ok := srcDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return errors.New("snapshot source is not SQLite")
			}
			dstSQLite, ok := dstDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return errors.New("snapshot destination is not SQLite")
			}
			backup, err := dstSQLite.Backup("main", srcSQLite, "main")
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, backup.Finish()) }()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				done, err := backup.Step(128)
				if err != nil {
					return err
				}
				if maxBytes > 0 && int64(backup.PageCount())*pageSize > maxBytes {
					return ErrBackupTooLarge
				}
				if done {
					return nil
				}
				timer := time.NewTimer(10 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		})
	})
	if err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	// The downloadable file must not depend on separate WAL or shared-memory files.
	// SQLite's backup of a never-written database can be zero bytes. Materialize
	// its header without adding user schema so it remains a valid standalone snapshot.
	info, err := os.Stat(destination)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		if _, err := dst.ExecContext(ctx, "PRAGMA user_version=0"); err != nil {
			return err
		}
	}
	_, err = dst.ExecContext(ctx, "PRAGMA journal_mode=DELETE")
	return err
}
