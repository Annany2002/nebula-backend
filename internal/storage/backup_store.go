package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Annany2002/nebula-backend/internal/core"
	"github.com/Annany2002/nebula-backend/internal/domain"
)

// Backup limits bound retained storage and reserve capacity for in-progress work.
const (
	MaxBackupsPerOwner        = 20
	MaxOwnerBackupBytes int64 = 256 << 20
)

// Backup errors are mapped to stable HTTP responses at the API boundary.
var (
	ErrInvalidBackupRequest = errors.New("invalid backup request")
	ErrBackupNotFound       = errors.New("backup not found")
	ErrBackupConflict       = errors.New("backup identifier already in use")
	ErrBackupBusy           = errors.New("backup operation in progress")
	ErrBackupQuota          = errors.New("backup storage quota exceeded")
	ErrBackupTooLarge       = errors.New("snapshot exceeds 64 MiB")
	ErrBackupUnavailable    = errors.New("backup file unavailable or integrity verification failed")
)

// BackupStore owns the durable snapshot lifecycle, independent of HTTP handlers.
type BackupStore struct {
	meta      *sql.DB
	directory string
	maxCount  int
	maxBytes  int64
	slots     chan struct{}
}

// NewBackupStore shares operation limits across callers of this store.
func NewBackupStore(meta *sql.DB, directory string) *BackupStore {
	return &BackupStore{meta: meta, directory: directory, maxCount: MaxBackupsPerOwner, maxBytes: MaxOwnerBackupBytes, slots: make(chan struct{}, 2)}
}

// IsValidBackupID accepts a canonical, nonzero UUID without path characters.
func IsValidBackupID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func validBackupOwner(owner string) bool {
	return owner != "" && owner != "." && owner != ".." && !strings.ContainsAny(owner, "/\\\x00") && filepath.Base(owner) == owner
}

func validBackupDatabaseName(name string) bool { return core.IsValidIdentifier(name) }

func validateBackupIdentity(owner, id string) error {
	if !validBackupOwner(owner) || !IsValidBackupID(id) {
		return ErrInvalidBackupRequest
	}
	return nil
}

func (s *BackupStore) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	default:
		return ErrBackupBusy
	}
}

// Create snapshots a source exactly once for a caller-chosen ID. A completed
// identical request returns its original metadata, even if the source was deleted.
func (s *BackupStore) Create(ctx context.Context, owner, name, id string) (backup domain.DatabaseBackup, created bool, resultErr error) {
	if err := validateBackupIdentity(owner, id); err != nil || !validBackupDatabaseName(name) {
		return backup, false, ErrInvalidBackupRequest
	}
	existing, err := s.Get(ctx, owner, id)
	if err == nil {
		if existing.DBName != name {
			return backup, false, ErrBackupConflict
		}
		if existing.Status != "ready" {
			return backup, false, ErrBackupBusy
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrBackupNotFound) {
		return backup, false, err
	}
	if err := s.acquire(ctx); err != nil {
		return backup, false, err
	}
	defer func() { <-s.slots }()
	release, err := LockDatabaseLifecycle(ctx, owner, name)
	if err != nil {
		return backup, false, err
	}
	defer release()
	source, err := s.reserve(ctx, owner, name, id)
	if err != nil {
		return backup, false, err
	}
	defer source.Close() //nolint:errcheck // Snapshot operations report all database errors.
	var ownStage, ownSnapshot bool
	defer func() {
		if resultErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, s.cleanupFiles(cleanupCtx, owner, id, ownStage, ownSnapshot))
		}
	}()
	backup, err = s.Get(ctx, owner, id)
	if err != nil {
		return backup, false, err
	}
	stage, snapshot, err := s.prepareDirectory(owner, id)
	if err != nil {
		return backup, false, err
	}
	stageFile, err := os.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return backup, false, err
	}
	ownStage = true
	if err := stageFile.Close(); err != nil {
		return backup, false, err
	}
	if err := createDatabaseSnapshot(ctx, source, stage, MaxSQLiteImportBytes); err != nil {
		return backup, false, err
	}
	size, digest, err := inspectCreatedBackup(ctx, stage)
	if err != nil {
		return backup, false, err
	}
	if err := os.Link(stage, snapshot); err != nil {
		return backup, false, err
	}
	ownSnapshot = true
	if err := os.Remove(stage); err != nil {
		return backup, false, err
	}
	if err := syncImportDirectory(filepath.Dir(snapshot)); err != nil {
		return backup, false, err
	}
	result, err := s.meta.ExecContext(ctx, "UPDATE database_backups SET status='ready',size_bytes=?,sha256=? WHERE owner_id=? AND backup_id=? AND status='creating'", size, digest, owner, id)
	if err != nil {
		return backup, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return backup, false, err
	}
	if count != 1 {
		return backup, false, ErrBackupConflict
	}
	// Once ready is committed, an interrupted response must not remove this backup.
	backup.Status, backup.SizeBytes, backup.SHA256 = "ready", size, digest
	return backup, true, nil
}

// Open verifies the saved bytes and returns a pinned file handle. Concurrent
// deletion cannot redirect a download/restore to a different file at the same path.
func (s *BackupStore) Open(ctx context.Context, owner, id string) (domain.DatabaseBackup, *os.File, error) {
	backup, err := s.Get(ctx, owner, id)
	if err != nil {
		return backup, nil, err
	}
	if backup.Status != "ready" {
		return backup, nil, ErrBackupBusy
	}
	if err := s.acquire(ctx); err != nil {
		return backup, nil, err
	}
	defer func() { <-s.slots }()
	directory, _, path, err := s.paths(owner, id)
	if err != nil {
		return backup, nil, err
	}
	for _, dir := range []string{filepath.Dir(directory), directory} {
		if err := requireBackupDirectory(dir); err != nil {
			return backup, nil, errors.Join(ErrBackupUnavailable, err)
		}
	}
	if err := requireRegularBackupFile(path); err != nil {
		return backup, nil, errors.Join(ErrBackupUnavailable, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return backup, nil, errors.Join(ErrBackupUnavailable, err)
	}
	size, digest, err := backupDigest(ctx, file)
	if errors.Is(err, ErrBackupTooLarge) {
		err = ErrBackupUnavailable
	}
	if err == nil && (size != backup.SizeBytes || digest != backup.SHA256) {
		err = ErrBackupUnavailable
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		return backup, nil, errors.Join(err, file.Close())
	}
	return backup, file, nil
}

// Restore copies verified immutable bytes to import staging; the existing import
// contract handles validation, registration and never-overwrite publication.
func (s *BackupStore) Restore(ctx context.Context, owner, id, name string) (int64, error) {
	if !validBackupDatabaseName(name) {
		return 0, ErrInvalidBackupRequest
	}
	backup, file, err := s.Open(ctx, owner, id)
	if err != nil {
		return 0, err
	}
	defer file.Close() //nolint:errcheck // Verified immutable source; no pending writes.
	if err := s.acquire(ctx); err != nil {
		return 0, err
	}
	defer func() { <-s.slots }()
	stage, err := os.CreateTemp(s.directory, ".nebula-import-*.db")
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = stage.Close()
		if err := removeBackupFile(stage.Name()); err != nil {
			customLog.Warnf("Restore staging cleanup failed: %v", err)
		}
	}()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(stage, digest), io.LimitReader(backupContextReader{ctx: ctx, source: file}, MaxSQLiteImportBytes+1))
	if err != nil {
		return 0, err
	}
	if size != backup.SizeBytes || hex.EncodeToString(digest.Sum(nil)) != backup.SHA256 {
		return 0, ErrBackupUnavailable
	}
	if err := stage.Sync(); err != nil {
		return 0, err
	}
	if err := stage.Close(); err != nil {
		return 0, err
	}
	return ImportSQLiteDatabase(ctx, s.meta, s.directory, owner, name, stage.Name())
}

// Delete retains a tombstone until file removal is durable. Repeated deletion of
// a missing owner-visible backup succeeds without revealing another owner's IDs.
func (s *BackupStore) Delete(ctx context.Context, owner, id string) error {
	backup, err := s.Get(ctx, owner, id)
	if errors.Is(err, ErrBackupNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if backup.Status == "creating" {
		return ErrBackupBusy
	}
	result, err := s.meta.ExecContext(ctx, "UPDATE database_backups SET status='deleting' WHERE owner_id=? AND backup_id=? AND status='ready'", owner, id)
	if err != nil {
		return err
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	return s.cleanup(ctx, owner, id)
}
