package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateEmptyDatabasePublishesFileBeforeRegistrationSuccess(t *testing.T) {
	f := newBackupFixture(t)
	require.NoError(t, CreateEmptyDatabase(t.Context(), f.store.meta, f.cfg.MetadataDbDir, "owner", "empty_native"))
	path, err := FindDatabasePath(t.Context(), f.store.meta, "owner", "empty_native")
	require.NoError(t, err)
	size, err := validateSQLiteSnapshot(t.Context(), path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, size, int64(512))
	_, _, err = f.store.Create(t.Context(), "owner", "empty_native", uuid.NewString())
	require.NoError(t, err)
	require.ErrorIs(t, CreateEmptyDatabase(t.Context(), f.store.meta, f.cfg.MetadataDbDir, "owner", "empty_native"), ErrDatabaseExists)
	stages, err := filepath.Glob(filepath.Join(f.cfg.MetadataDbDir, ".sqlite-create-*"))
	require.NoError(t, err)
	require.Empty(t, stages)
}

func TestCreateEmptyDatabaseRejectsOrphanFilesAndSidecars(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			f := newBackupFixture(t)
			directory := filepath.Join(f.cfg.MetadataDbDir, "owner")
			require.NoError(t, os.MkdirAll(directory, 0o750))
			path := filepath.Join(directory, "collision.db") + suffix
			require.NoError(t, os.WriteFile(path, []byte("retained"), 0o600))
			require.ErrorIs(t, CreateEmptyDatabase(t.Context(), f.store.meta, f.cfg.MetadataDbDir, "owner", "collision"), ErrDatabaseExists)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "retained", string(data))
			_, err = FindDatabasePath(t.Context(), f.store.meta, "owner", "collision")
			require.ErrorIs(t, err, ErrDatabaseNotFound)
		})
	}
}

func TestBackupMissingSourceFileCannotBeAssumedEmpty(t *testing.T) {
	f := newBackupFixture(t)
	path := filepath.Join(f.cfg.MetadataDbDir, "missing.db")
	require.NoError(t, RegisterDatabase(t.Context(), f.store.meta, "owner", "missing", path))
	_, _, err := f.store.Create(t.Context(), "owner", "missing", uuid.NewString())
	require.Error(t, err)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	backups, total, err := f.store.List(t.Context(), "owner", "missing", 20, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, backups)
}

func TestCreateEmptyDatabaseCancellationLeavesNoRegistrationOrStage(t *testing.T) {
	f := newBackupFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, CreateEmptyDatabase(ctx, f.store.meta, f.cfg.MetadataDbDir, "owner", "cancelled"), context.Canceled)
	_, err := FindDatabasePath(t.Context(), f.store.meta, "owner", "cancelled")
	require.ErrorIs(t, err, ErrDatabaseNotFound)
	stages, err := filepath.Glob(filepath.Join(f.cfg.MetadataDbDir, ".sqlite-create-*"))
	require.NoError(t, err)
	require.Empty(t, stages)
}

func TestCreateEmptyDatabaseRejectsDestinationSymlink(t *testing.T) {
	f := newBackupFixture(t)
	directory := filepath.Join(f.cfg.MetadataDbDir, "owner")
	require.NoError(t, os.MkdirAll(directory, 0o750))
	target := filepath.Join(f.cfg.MetadataDbDir, "retained.txt")
	require.NoError(t, os.WriteFile(target, []byte("retained"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(directory, "symlink.db")))
	require.ErrorIs(t, CreateEmptyDatabase(t.Context(), f.store.meta, f.cfg.MetadataDbDir, "owner", "symlink"), ErrDatabaseExists)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "retained", string(data))
	_, err = FindDatabasePath(t.Context(), f.store.meta, "owner", "symlink")
	require.ErrorIs(t, err, ErrDatabaseNotFound)
}
