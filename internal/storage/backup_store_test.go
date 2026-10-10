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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

type backupFixture struct {
	store  *BackupStore
	source *sql.DB
	path   string
	cfg    *config.Config
}

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	meta, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, meta.Close()) })
	_, err = CreateUser(t.Context(), meta, "owner", "builder", "backup@example.test", "test-hash")
	require.NoError(t, err)
	path := filepath.Join(cfg.MetadataDbDir, "source.db")
	require.NoError(t, RegisterDatabase(t.Context(), meta, "owner", "source", path))
	source, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, source.Close()) })
	_, err = source.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY AUTOINCREMENT,label TEXT UNIQUE,payload BLOB,optional TEXT);
 CREATE TABLE audit(item_id INTEGER);
 CREATE TABLE related(item_id INTEGER REFERENCES items(id));
 CREATE INDEX item_lookup ON related(item_id);
 CREATE VIEW item_view AS SELECT id,label FROM items;
 CREATE TRIGGER item_audit AFTER INSERT ON items BEGIN INSERT INTO audit VALUES(NEW.id); END;
 INSERT INTO items(label,payload,optional) VALUES('committed-in-wal',X'00FF',NULL);
 INSERT INTO related VALUES(1);
 INSERT INTO items(id,label) VALUES(10,'sequence-gap'); DELETE FROM items WHERE id=10;`)
	require.NoError(t, err)
	return &backupFixture{store: NewBackupStore(meta, cfg.MetadataDbDir), source: source, path: path, cfg: cfg}
}

func (f *backupFixture) create(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	backup, created, err := f.store.Create(t.Context(), "owner", "source", id)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "ready", backup.Status)
	require.Equal(t, id, backup.BackupID)
	require.Len(t, backup.SHA256, 64)
	require.Positive(t, backup.SizeBytes)
	return id
}

func TestBackupPreservesWALAndObjectsAndSurvivesSourceDeletion(t *testing.T) {
	f := newBackupFixture(t)
	id := f.create(t)
	backup, file, err := f.store.Open(t.Context(), "owner", id)
	require.NoError(t, err)
	bytes, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	digest := sha256.Sum256(bytes)
	require.Equal(t, hex.EncodeToString(digest[:]), backup.SHA256)
	require.Equal(t, int64(len(bytes)), backup.SizeBytes)
	_, err = f.source.Exec("INSERT INTO items(label) VALUES('after-backup')")
	require.NoError(t, err)
	replay, created, err := f.store.Create(t.Context(), "owner", "source", id)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, backup, replay)
	_, err = f.store.Restore(t.Context(), "owner", id, "source")
	require.ErrorIs(t, err, ErrDatabaseExists)
	require.NoError(t, f.source.Close())
	require.NoError(t, DeleteDatabaseRegistration(t.Context(), f.store.meta, "owner", "source"))
	require.NoError(t, os.Remove(f.path))
	list, total, err := f.store.List(t.Context(), "owner", "source", 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, list, 1)
	size, err := f.store.Restore(t.Context(), "owner", id, "restored")
	require.NoError(t, err)
	require.Equal(t, backup.SizeBytes, size)
	path, err := FindDatabasePath(t.Context(), f.store.meta, "owner", "restored")
	require.NoError(t, err)
	restored, err := ConnectUserDB(t.Context(), path)
	require.NoError(t, err)
	defer restored.Close()
	var count int
	require.NoError(t, restored.QueryRow("SELECT count(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
	var payload, optional string
	require.NoError(t, restored.QueryRow("SELECT hex(payload),typeof(optional) FROM items WHERE id=1").Scan(&payload, &optional))
	require.Equal(t, "00FF", payload)
	require.Equal(t, "null", optional)
	require.NoError(t, restored.QueryRow("SELECT count(*) FROM item_view").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, restored.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('item_lookup','item_audit')").Scan(&count))
	require.Equal(t, 2, count)
	_, err = restored.Exec("INSERT INTO items(label) VALUES('restored-write')")
	require.NoError(t, err)
	var lastID int
	require.NoError(t, restored.QueryRow("SELECT id FROM items WHERE label='restored-write'").Scan(&lastID))
	require.Equal(t, 11, lastID)
	require.NoError(t, restored.QueryRow("SELECT count(*) FROM audit WHERE item_id=11").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.store.meta.QueryRow("SELECT count(*) FROM api_keys").Scan(&count))
	require.Zero(t, count)
	_, _, err = f.store.Create(t.Context(), "owner", "source", id)
	require.NoError(t, err, "completed replay remains available after source deletion")
}

func TestBackupEmptyDatabaseProducesImportableSnapshot(t *testing.T) {
	f := newBackupFixture(t)
	emptyPath := filepath.Join(f.cfg.MetadataDbDir, "empty.db")
	empty, err := sql.Open("sqlite3", emptyPath)
	require.NoError(t, err)
	require.NoError(t, empty.Ping())
	require.NoError(t, empty.Close())
	require.NoError(t, RegisterDatabase(t.Context(), f.store.meta, "owner", "empty", emptyPath))
	id := uuid.NewString()
	backup, _, err := f.store.Create(t.Context(), "owner", "empty", id)
	require.NoError(t, err)
	require.GreaterOrEqual(t, backup.SizeBytes, int64(512))
	_, err = f.store.Restore(t.Context(), "owner", id, "empty_copy")
	require.NoError(t, err)
}

func TestBackupTenantIsolationAndIdentifierValidation(t *testing.T) {
	f := newBackupFixture(t)
	id := f.create(t)
	_, err := CreateUser(t.Context(), f.store.meta, "other", "otheruser", "other@example.test", "hash")
	require.NoError(t, err)
	_, err = f.store.Get(t.Context(), "other", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
	_, _, err = f.store.Open(t.Context(), "other", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
	_, err = f.store.Restore(t.Context(), "other", id, "stolen")
	require.ErrorIs(t, err, ErrBackupNotFound)
	require.NoError(t, f.store.Delete(t.Context(), "other", id))
	_, err = f.store.Get(t.Context(), "owner", id)
	require.NoError(t, err)
	_, _, err = f.store.Create(t.Context(), "other", "source", uuid.NewString())
	require.ErrorIs(t, err, ErrDatabaseNotFound)
	for _, badID := range []string{"", "../escape", uuid.Nil.String(), "urn:uuid:" + id, "{" + id + "}"} {
		_, err := f.store.Get(t.Context(), "owner", badID)
		require.ErrorIs(t, err, ErrInvalidBackupRequest)
	}
	for _, owner := range []string{"..", "../outside", "x/y", "x\\y"} {
		_, err := f.store.Get(t.Context(), owner, id)
		require.ErrorIs(t, err, ErrInvalidBackupRequest)
	}
	for _, name := range []string{"", "../outside", "two words"} {
		_, _, err = f.store.Create(t.Context(), "owner", name, uuid.NewString())
		require.ErrorIs(t, err, ErrInvalidBackupRequest)
	}
	_, _, err = f.store.Create(t.Context(), "owner", "different", id)
	require.ErrorIs(t, err, ErrBackupConflict)
}

func TestBackupIntegrityMissingAndSymlinkFailuresNeverRestore(t *testing.T) {
	for _, failure := range []string{"corrupt", "missing", "symlink"} {
		t.Run(failure, func(t *testing.T) {
			f := newBackupFixture(t)
			id := f.create(t)
			_, _, snapshot, err := f.store.paths("owner", id)
			require.NoError(t, err)
			switch failure {
			case "corrupt":
				file, err := os.OpenFile(snapshot, os.O_WRONLY, 0)
				require.NoError(t, err)
				_, err = file.WriteAt([]byte("corrupt"), 120)
				require.NoError(t, err)
				require.NoError(t, file.Close())
			case "missing":
				require.NoError(t, os.Remove(snapshot))
			case "symlink":
				require.NoError(t, os.Remove(snapshot))
				require.NoError(t, os.Symlink(f.path, snapshot))
			}
			_, _, err = f.store.Open(t.Context(), "owner", id)
			require.ErrorIs(t, err, ErrBackupUnavailable)
			_, err = f.store.Restore(t.Context(), "owner", id, "bad_restore")
			require.ErrorIs(t, err, ErrBackupUnavailable)
			_, err = FindDatabasePath(t.Context(), f.store.meta, "owner", "bad_restore")
			require.ErrorIs(t, err, ErrDatabaseNotFound)
			require.NoError(t, f.store.Delete(t.Context(), "owner", id))
			_, err = os.Stat(f.path)
			require.NoError(t, err, "deleting a symlink must not remove its target")
		})
	}
}

func TestBackupFileCollisionIsPreserved(t *testing.T) {
	f := newBackupFixture(t)
	id := uuid.NewString()
	dir, _, snapshot, err := f.store.paths("owner", id)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(snapshot, []byte("operator-preserved-orphan"), 0o600))
	_, _, err = f.store.Create(t.Context(), "owner", "source", id)
	require.ErrorIs(t, err, ErrBackupConflict)
	bytes, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	require.Equal(t, "operator-preserved-orphan", string(bytes))
	_, err = f.store.Get(t.Context(), "owner", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
}

func TestBackupDeletedIDsCannotBeReusedAndOpenHandleSurvivesDelete(t *testing.T) {
	f := newBackupFixture(t)
	id := f.create(t)
	backup, file, err := f.store.Open(t.Context(), "owner", id)
	require.NoError(t, err)
	defer file.Close()
	require.NoError(t, f.store.Delete(t.Context(), "owner", id))
	require.NoError(t, f.store.Delete(t.Context(), "owner", id))
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, backup.SizeBytes, int64(len(data)))
	_, _, err = f.store.Create(t.Context(), "owner", "source", id)
	require.ErrorIs(t, err, ErrBackupConflict)
	list, total, err := f.store.List(t.Context(), "owner", "", 20, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, list)
}

func TestBackupQuotaAndConcurrentReservations(t *testing.T) {
	f := newBackupFixture(t)
	f.store.maxCount = 1
	start := make(chan struct{})
	out := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := f.store.Create(t.Context(), "owner", "source", uuid.NewString())
			out <- err
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	successes, quotas := 0, 0
	for err := range out {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrBackupQuota) {
			quotas++
		} else {
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, quotas)
	list, total, err := f.store.List(t.Context(), "owner", "", 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.NoError(t, f.store.Delete(t.Context(), "owner", list[0].BackupID))
	f.create(t)
	f.store.maxCount = 20
	f.store.maxBytes = MaxSQLiteImportBytes
	_, _, err = f.store.Create(t.Context(), "owner", "source", uuid.NewString())
	require.ErrorIs(t, err, ErrBackupQuota, "reserve the full maximum before a new snapshot")
}

func TestBackupCancelledInvalidAndOversizeCreationsReleaseReservations(t *testing.T) {
	f := newBackupFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := f.store.Create(ctx, "owner", "source", uuid.NewString())
	require.ErrorIs(t, err, context.Canceled)
	// A small, configurable internal byte limit exercises growth bounds without a huge fixture.
	stage := filepath.Join(t.TempDir(), "bounded.db")
	err = createDatabaseSnapshot(t.Context(), f.source, stage, 512)
	require.ErrorIs(t, err, ErrBackupTooLarge)
	_, err = f.source.Exec("PRAGMA foreign_keys=OFF; UPDATE related SET item_id=999")
	require.NoError(t, err)
	id := uuid.NewString()
	_, _, err = f.store.Create(t.Context(), "owner", "source", id)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
	_, err = f.store.Get(t.Context(), "owner", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
	_, stage, snapshot, err := f.store.paths("owner", id)
	require.NoError(t, err)
	for _, path := range []string{stage, snapshot} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestBackupCapacityAndPaging(t *testing.T) {
	f := newBackupFixture(t)
	first := f.create(t)
	second := f.create(t)
	list, total, err := f.store.List(t.Context(), "owner", "source", 1, 1)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, list, 1)
	_, total, err = f.store.List(t.Context(), "owner", "absent", 20, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	for _, pair := range [][2]int{{0, 0}, {101, 0}, {20, -1}, {20, 1000001}} {
		_, _, err = f.store.List(t.Context(), "owner", "", pair[0], pair[1])
		require.ErrorIs(t, err, ErrInvalidBackupRequest)
	}
	f.store.slots <- struct{}{}
	f.store.slots <- struct{}{}
	_, _, err = f.store.Create(t.Context(), "owner", "source", uuid.NewString())
	require.ErrorIs(t, err, ErrBackupBusy)
	_, _, err = f.store.Open(t.Context(), "owner", first)
	require.ErrorIs(t, err, ErrBackupBusy)
	<-f.store.slots
	<-f.store.slots
	_, err = f.store.Get(t.Context(), "owner", second)
	require.NoError(t, err)
}

func TestBackupRecoveryCleansInterruptedStatesAndKeepsReady(t *testing.T) {
	f := newBackupFixture(t)
	ready := f.create(t)
	creating, deleting := uuid.NewString(), f.create(t)
	_, err := f.store.meta.Exec("INSERT INTO database_backups(backup_id,owner_id,source_database_id,db_name,status) VALUES(?,'owner',1,'source','creating');", creating)
	require.NoError(t, err)
	_, stage, snapshot, err := f.store.paths("owner", creating)
	require.NoError(t, err)
	for _, path := range []string{stage, stage + "-journal", snapshot} {
		require.NoError(t, os.WriteFile(path, []byte("interrupted"), 0o600))
	}
	_, err = f.store.meta.Exec("UPDATE database_backups SET status='deleting' WHERE backup_id=?", deleting)
	require.NoError(t, err)
	require.NoError(t, f.source.Close())
	require.NoError(t, f.store.meta.Close())
	meta, err := ConnectMetadataDB(f.cfg)
	require.NoError(t, err)
	defer meta.Close()
	recovered := NewBackupStore(meta, f.cfg.MetadataDbDir)
	for _, id := range []string{creating, deleting} {
		_, err = recovered.Get(t.Context(), "owner", id)
		require.ErrorIs(t, err, ErrBackupNotFound)
	}
	_, _, err = recovered.Create(t.Context(), "owner", "source", deleting)
	require.ErrorIs(t, err, ErrBackupConflict)
	_, file, err := recovered.Open(t.Context(), "owner", ready)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	for _, path := range []string{stage, stage + "-journal", snapshot} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	require.NoError(t, recovered.Recover(t.Context()))
}

func TestBackupMetadataFailureDoesNotPublishAndDoesNotDeleteCommittedReady(t *testing.T) {
	f := newBackupFixture(t)
	_, err := f.store.meta.Exec(`CREATE TRIGGER reject_ready BEFORE UPDATE OF status ON database_backups WHEN NEW.status='ready' BEGIN SELECT RAISE(ABORT,'forced metadata failure'); END;`)
	require.NoError(t, err)
	id := uuid.NewString()
	_, _, err = f.store.Create(t.Context(), "owner", "source", id)
	require.ErrorContains(t, err, "forced metadata failure")
	_, stage, snapshot, err := f.store.paths("owner", id)
	require.NoError(t, err)
	for _, path := range []string{stage, snapshot} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	_, err = f.store.Get(t.Context(), "owner", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
	_, err = f.store.meta.Exec("DROP TRIGGER reject_ready")
	require.NoError(t, err)
	ready := f.create(t)
	require.NoError(t, f.store.cleanup(t.Context(), "owner", ready), "an ambiguous success cleanup cannot remove a committed backup")
	_, file, err := f.store.Open(t.Context(), "owner", ready)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestBackupConcurrentIdenticalIDsDoNotCreateCopies(t *testing.T) {
	f := newBackupFixture(t)
	id := uuid.NewString()
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, _, err := f.store.Create(t.Context(), "owner", "source", id); results <- err }()
	}
	close(start)
	for range 2 {
		err := <-results
		require.True(t, err == nil || errors.Is(err, ErrBackupConflict) || errors.Is(err, ErrBackupBusy), fmt.Sprint(err))
	}
	list, total, err := f.store.List(t.Context(), "owner", "", 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, list, 1)
	require.Equal(t, "ready", list[0].Status)
}

func TestBackupCancelledAfterReservationCleansState(t *testing.T) {
	f := newBackupFixture(t)
	id := uuid.NewString()
	source, err := f.store.reserve(t.Context(), "owner", "source", id)
	require.NoError(t, err)
	require.NoError(t, source.Close())
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	_, _, err = f.store.Create(ctx, "owner", "source", id)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, f.store.Recover(t.Context()))
	_, err = f.store.Get(t.Context(), "owner", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
}

func TestBackupCancellationDuringSnapshotReleasesFilesAndCapacity(t *testing.T) {
	f := newBackupFixture(t)
	_, err := f.source.Exec("INSERT INTO items(label,payload) VALUES('large',zeroblob(8388608))")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	id := uuid.NewString()
	_, stage, snapshot, err := f.store.paths("owner", id)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { _, _, err := f.store.Create(ctx, "owner", "source", id); result <- err }()
	require.Eventually(t, func() bool { _, err := os.Stat(stage); return err == nil }, 3*time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	for _, path := range []string{stage, snapshot} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	_, err = f.store.Get(t.Context(), "owner", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
	require.Empty(t, f.store.slots)
	release, err := LockDatabaseLifecycle(t.Context(), "owner", "source")
	require.NoError(t, err)
	release()
}

func TestBackupDirectorySymlinkDoesNotFollowOrLeakQuota(t *testing.T) {
	f := newBackupFixture(t)
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(f.cfg.MetadataDbDir, ".nebula-backups")))
	id := uuid.NewString()
	_, _, err := f.store.Create(t.Context(), "owner", "source", id)
	require.ErrorIs(t, err, ErrBackupUnavailable)
	_, err = f.store.Get(t.Context(), "owner", id)
	require.ErrorIs(t, err, ErrBackupNotFound)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestDatabaseLifecycleLocksRespectCancellationAndAreReleased(t *testing.T) {
	release, err := LockDatabaseLifecycle(t.Context(), "lock-owner", "source")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err = LockDatabaseLifecycle(ctx, "lock-owner", "source")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	otherRelease, err := LockDatabaseLifecycle(t.Context(), "lock-owner", "other")
	require.NoError(t, err)
	otherRelease()
	release()
	release()
	databaseLifecycleLocks.Lock()
	defer databaseLifecycleLocks.Unlock()
	require.NotContains(t, databaseLifecycleLocks.entries, "lock-owner\x00source")
	require.NotContains(t, databaseLifecycleLocks.entries, "lock-owner\x00other")
}

func TestBackupAfterStudioTableListingPreservesInternalTimestamps(t *testing.T) {
	f := newBackupFixture(t)
	tables, err := ListTables(t.Context(), f.source)
	require.NoError(t, err)
	require.NotEmpty(t, tables)
	var timestamp string
	require.NoError(t, f.source.QueryRow("SELECT created_at FROM _nebula_table_metadata WHERE table_name='items'").Scan(&timestamp))
	id := f.create(t)
	_, err = f.store.Restore(t.Context(), "owner", id, "from_studio")
	require.NoError(t, err)
	path, err := FindDatabasePath(t.Context(), f.store.meta, "owner", "from_studio")
	require.NoError(t, err)
	restored, err := ConnectUserDB(t.Context(), path)
	require.NoError(t, err)
	defer restored.Close()
	var actual string
	require.NoError(t, restored.QueryRow("SELECT created_at FROM _nebula_table_metadata WHERE table_name='items'").Scan(&actual))
	require.Equal(t, timestamp, actual)
	tables, err = ListTables(t.Context(), restored)
	require.NoError(t, err)
	require.Len(t, tables, 3)
}
