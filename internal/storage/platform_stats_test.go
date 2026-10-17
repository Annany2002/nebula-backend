package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformStatsCountsLiveWALTablesAndVirtualTablesWithoutShadows(t *testing.T) {
	db, cfg := accountStatusFixture(t)
	ctx := context.Background()
	path := filepath.Join(cfg.MetadataDbDir, "tenant.db")
	tenant, err := ConnectUserDB(ctx, path)
	require.NoError(t, err)
	defer tenant.Close()
	_, err = tenant.Exec(`PRAGMA wal_autocheckpoint=0;
 CREATE TABLE items(id INTEGER PRIMARY KEY AUTOINCREMENT,label TEXT);
 CREATE TABLE sqliteX_user(value TEXT);
 CREATE TABLE _nebulax_user(value TEXT);
 CREATE TABLE _nebula_table_metadata(table_name TEXT PRIMARY KEY,created_at TIMESTAMP);
 CREATE VIRTUAL TABLE search USING fts4(content);
 CREATE VIEW item_view AS SELECT * FROM items;
 CREATE INDEX item_label ON items(label);
 INSERT INTO items(label) VALUES('preserved');`)
	require.NoError(t, err)
	require.NoError(t, RegisterDatabase(ctx, db, "target", "tenant", path))
	require.FileExists(t, path+"-wal")
	stats, err := GetPlatformStats(ctx, db, cfg.MetadataDbDir)
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.TotalUsers)
	require.EqualValues(t, 1, stats.TotalDatabases)
	require.NotNil(t, stats.TotalTables)
	require.EqualValues(t, 4, *stats.TotalTables)
	require.EqualValues(t, 1, stats.DatabasesCounted)
	require.Zero(t, stats.DatabasesUnavailable)
	var label string
	require.NoError(t, tenant.QueryRow(`SELECT label FROM items`).Scan(&label))
	require.Equal(t, "preserved", label)
	_, err = tenant.Exec(`CREATE TABLE committed_in_wal(value TEXT)`)
	require.NoError(t, err)
	stats, err = GetPlatformStats(ctx, db, cfg.MetadataDbDir)
	require.NoError(t, err)
	require.EqualValues(t, 5, *stats.TotalTables)
	count, err := countTenantTables(ctx, cfg.MetadataDbDir, path)
	require.NoError(t, err)
	require.EqualValues(t, 5, count)
}

func TestPlatformStatsUnavailableFilesAreNotCreatedOrReportedAsZero(t *testing.T) {
	db, cfg := accountStatusFixture(t)
	ctx := context.Background()
	stats, err := GetPlatformStats(ctx, db, cfg.MetadataDbDir)
	require.NoError(t, err)
	require.NotNil(t, stats.TotalTables)
	require.Zero(t, *stats.TotalTables)
	missing := filepath.Join(cfg.MetadataDbDir, "missing.db")
	corrupt := filepath.Join(cfg.MetadataDbDir, "corrupt.db")
	require.NoError(t, os.WriteFile(corrupt, []byte("not SQLite"), 0o600))
	outside := filepath.Join(t.TempDir(), "outside.db")
	require.NoError(t, os.WriteFile(outside, []byte("must not be opened"), 0o600))
	link := filepath.Join(cfg.MetadataDbDir, "link.db")
	require.NoError(t, os.Symlink(outside, link))
	for name, path := range map[string]string{"missing": missing, "corrupt": corrupt, "outside": outside, "link": link, "directory": cfg.MetadataDbDir} {
		require.NoError(t, RegisterDatabase(ctx, db, "target", name, path))
	}
	stats, err = GetPlatformStats(ctx, db, cfg.MetadataDbDir)
	require.NoError(t, err)
	require.Nil(t, stats.TotalTables)
	require.EqualValues(t, 5, stats.TotalDatabases)
	require.EqualValues(t, 5, stats.DatabasesUnavailable)
	require.Zero(t, stats.DatabasesCounted)
	require.NoFileExists(t, missing)
	bytes, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "must not be opened", string(bytes))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = GetPlatformStats(cancelled, db, cfg.MetadataDbDir)
	require.ErrorIs(t, err, context.Canceled)
}
