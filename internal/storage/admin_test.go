package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

func TestAdminMembershipMigrationPersistenceAndCascade(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	// Simulate a database created before admin membership existed.
	legacy, err := sql.Open("sqlite3", filepath.Join(cfg.MetadataDbDir, cfg.MetadataDbFile))
	require.NoError(t, err)
	_, err = legacy.Exec(`CREATE TABLE users(user_id TEXT PRIMARY KEY NOT NULL,username TEXT NOT NULL,email TEXT UNIQUE NOT NULL,password_hash TEXT NOT NULL,created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
	 INSERT INTO users(user_id,username,email,password_hash) VALUES('legacy','legacy','legacy@example.test','secret');`)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())
	db, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	allowed, err := IsPlatformAdmin(ctx, db, "legacy")
	require.NoError(t, err)
	require.False(t, allowed, "Migrating must not promote the first or any existing user")
	for range 2 {
		require.NoError(t, SetPlatformAdmin(ctx, db, "legacy", true))
	}
	for _, grant := range []bool{true, false} {
		require.ErrorIs(t, SetPlatformAdmin(ctx, db, "missing", grant), ErrUserNotFound)
	}
	require.NoError(t, db.Close())
	db, err = ConnectMetadataDB(cfg)
	require.NoError(t, err)
	allowed, err = IsPlatformAdmin(ctx, db, "legacy")
	require.NoError(t, err)
	require.True(t, allowed)
	for range 2 {
		require.NoError(t, SetPlatformAdmin(ctx, db, "legacy", false))
	}
	allowed, err = IsPlatformAdmin(ctx, db, "legacy")
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, SetPlatformAdmin(ctx, db, "legacy", true))
	_, err = db.Exec(`DELETE FROM users WHERE user_id='legacy'`)
	require.NoError(t, err)
	allowed, err = IsPlatformAdmin(ctx, db, "legacy")
	require.NoError(t, err)
	require.False(t, allowed, "Deleting an account must also remove its membership")
}

func TestAdminQueriesAreOrderedBoundedAndMetadataOnly(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	db, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer db.Close()
	users, total, err := ListAdminUsers(ctx, db, 20, 0)
	require.NoError(t, err)
	require.NotNil(t, users)
	require.Empty(t, users)
	require.Zero(t, total)
	for _, id := range []string{"c", "a", "b"} {
		_, err = CreateUser(ctx, db, id, id, id+"@example.test", "private-hash")
		require.NoError(t, err)
	}
	_, err = db.Exec(`UPDATE users SET created_at='2026-10-17 10:00:00'; UPDATE users SET created_at='2026-10-16 10:00:00' WHERE user_id='c'`)
	require.NoError(t, err)
	require.NoError(t, SetPlatformAdmin(ctx, db, "a", true))
	// Register missing tenant files: admin reads must neither open nor create them.
	missingFile := filepath.Join(cfg.MetadataDbDir, "never-create.db")
	require.NoError(t, RegisterDatabase(ctx, db, "b", "first", missingFile))
	require.NoError(t, RegisterDatabase(ctx, db, "b", "second", filepath.Join(cfg.MetadataDbDir, "also-missing.db")))
	_, err = db.Exec(`INSERT INTO account_email_verifications(user_id,verified_email,required) VALUES('b','b@example.test',1)`)
	require.NoError(t, err)
	users, total, err = ListAdminUsers(ctx, db, 2, 0)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Len(t, users, 2)
	require.Equal(t, "a", users[0].UserID)
	require.Equal(t, "admin", users[0].Role)
	require.False(t, users[0].EmailVerified)
	require.Equal(t, "b", users[1].UserID)
	require.EqualValues(t, 2, users[1].DatabaseCount)
	require.True(t, users[1].EmailVerified)
	require.True(t, users[1].VerificationRequired)
	user, err := GetAdminUser(ctx, db, "b")
	require.NoError(t, err)
	require.Equal(t, users[1], user)
	users, total, err = ListAdminUsers(ctx, db, 2, 2)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Len(t, users, 1)
	require.Equal(t, "c", users[0].UserID)
	users, total, err = ListAdminUsers(ctx, db, 2, 1_000_000)
	require.NoError(t, err)
	require.Empty(t, users)
	require.NotNil(t, users)
	require.EqualValues(t, 3, total)
	stats, err := GetPlatformStats(ctx, db, cfg.MetadataDbDir)
	require.NoError(t, err)
	require.EqualValues(t, 3, stats.TotalUsers)
	require.EqualValues(t, 2, stats.TotalDatabases)
	require.NoFileExists(t, missingFile)
	_, err = GetAdminUser(ctx, db, "' OR 1=1 --")
	require.ErrorIs(t, err, ErrUserNotFound)
	for _, page := range [][2]int{{0, 0}, {101, 0}, {20, -1}, {20, 1_000_001}} {
		_, _, err = ListAdminUsers(ctx, db, page[0], page[1])
		require.ErrorIs(t, err, ErrInvalidAdminPage)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = ListAdminUsers(cancelled, db, 20, 0)
	require.ErrorIs(t, err, context.Canceled)
	_, err = GetPlatformStats(cancelled, db, cfg.MetadataDbDir)
	require.ErrorIs(t, err, context.Canceled)
	_, err = GetAdminUser(cancelled, db, "b")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, SetPlatformAdmin(cancelled, db, "a", false), context.Canceled)
}
