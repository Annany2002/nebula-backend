package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (s *BackupStore) paths(owner, id string) (directory, stage, snapshot string, err error) {
	if err := validateBackupIdentity(owner, id); err != nil {
		return "", "", "", err
	}
	directory = filepath.Join(s.directory, ".nebula-backups", owner)
	return directory, filepath.Join(directory, ".stage-"+id), filepath.Join(directory, id+".db"), nil
}

func (s *BackupStore) prepareDirectory(owner, id string) (stage, snapshot string, err error) {
	directory, stage, snapshot, err := s.paths(owner, id)
	if err != nil {
		return "", "", err
	}
	for _, path := range []string{filepath.Dir(directory), directory} {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", "", err
		}
		if err := requireBackupDirectory(path); err != nil {
			return "", "", err
		}
		if err := syncImportDirectory(filepath.Dir(path)); err != nil {
			return "", "", err
		}
	}
	for _, path := range []string{stage, stage + "-journal", stage + "-wal", stage + "-shm", snapshot} {
		if _, err := os.Lstat(path); err == nil {
			return "", "", ErrBackupConflict
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
	}
	return stage, snapshot, nil
}

func requireBackupDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: storage directory is not a real directory", ErrBackupUnavailable)
	}
	return nil
}

func requireRegularBackupFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrBackupUnavailable
	}
	return nil
}

type backupContextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r backupContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(data)
}

func backupDigest(ctx context.Context, file *os.File) (int64, string, error) {
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(backupContextReader{ctx: ctx, source: file}, MaxSQLiteImportBytes+1))
	if count > MaxSQLiteImportBytes {
		return 0, "", ErrBackupTooLarge
	}
	if err == nil {
		err = ctx.Err()
	}
	return count, hex.EncodeToString(digest.Sum(nil)), err
}

func inspectCreatedBackup(ctx context.Context, path string) (int64, string, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, "", err
	}
	defer file.Close() //nolint:errcheck // Sync below reports durability failures.
	if err := file.Sync(); err != nil {
		return 0, "", err
	}
	size, digest, err := backupDigest(ctx, file)
	if err != nil {
		return 0, "", err
	}
	if _, err := validateSQLiteSnapshot(ctx, path); err != nil {
		return 0, "", err
	}
	return size, digest, nil
}

func (s *BackupStore) cleanup(ctx context.Context, owner, id string) error {
	return s.cleanupFiles(ctx, owner, id, true, true)
}

func (s *BackupStore) cleanupFiles(ctx context.Context, owner, id string, ownStage, ownSnapshot bool) error {
	var status string
	err := s.meta.QueryRowContext(ctx, "SELECT status FROM database_backups WHERE owner_id=? AND backup_id=?", owner, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && status != "creating" && status != "deleting") {
		return nil
	}
	if err != nil {
		return err
	}
	if !ownStage && !ownSnapshot {
		return s.forget(ctx, owner, id)
	}
	directory, stage, snapshot, err := s.paths(owner, id)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Dir(directory)); errors.Is(err, os.ErrNotExist) {
		return s.forget(ctx, owner, id)
	} else if err != nil {
		return err
	}
	if err := requireBackupDirectory(filepath.Dir(directory)); err != nil {
		return err
	}
	if _, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		return s.forget(ctx, owner, id)
	} else if err != nil {
		return err
	}
	if err := requireBackupDirectory(directory); err != nil {
		return err
	}
	paths := make([]string, 0, 5)
	if ownStage {
		paths = append(paths, stage, stage+"-journal", stage+"-wal", stage+"-shm")
	}
	if ownSnapshot {
		paths = append(paths, snapshot)
	}
	for _, path := range paths {
		if err := removeBackupFile(path); err != nil {
			return err
		}
	}
	if err := syncImportDirectory(directory); err != nil {
		return err
	}
	return s.forget(ctx, owner, id)
}

func (s *BackupStore) forget(ctx context.Context, owner, id string) error {
	// Keep deleted IDs reserved: a delayed duplicate delete must never remove a new snapshot.
	if _, err := s.meta.ExecContext(ctx, "UPDATE database_backups SET status='deleted',size_bytes=0,sha256='' WHERE owner_id=? AND backup_id=? AND status='deleting'", owner, id); err != nil {
		return err
	}
	_, err := s.meta.ExecContext(ctx, "DELETE FROM database_backups WHERE owner_id=? AND backup_id=? AND status='creating'", owner, id)
	return err
}
