package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestAdminCommandRequiresExplicitExistingDatabaseAndUser(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	db, err := storage.ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer db.Close()
	_, err = storage.CreateUser(ctx, db, "owner", "owner", "owner@example.test", "private")
	require.NoError(t, err)
	path := filepath.Join(cfg.MetadataDbDir, "metadata.db")
	var output bytes.Buffer
	for _, action := range []string{"grant", "grant", "revoke", "revoke"} {
		require.NoError(t, run([]string{"--metadata-db", path, "--user-id", "owner", action}, &output, io.Discard))
		allowed, checkErr := storage.IsPlatformAdmin(ctx, db, "owner")
		require.NoError(t, checkErr)
		require.Equal(t, action == "grant", allowed)
	}
	require.NotContains(t, output.String(), "private")
	require.NotContains(t, output.String(), "owner@example.test")
	missing := filepath.Join(cfg.MetadataDbDir, "missing.db")
	for _, args := range [][]string{
		nil,
		{"--user-id", "owner", "grant"},
		{"--metadata-db", path, "grant"},
		{"--metadata-db", path, "--user-id", "owner", "promote"},
		{"--metadata-db", path, "--user-id", "owner", "grant", "extra"},
		{"--metadata-db", path, "--user-id", "missing", "grant"},
		{"--metadata-db", path, "--user-id", "missing", "revoke"},
		{"--metadata-db", missing, "--user-id", "owner", "grant"},
	} {
		require.Error(t, run(args, io.Discard, io.Discard), args)
	}
	require.NoFileExists(t, missing)
	// URI metacharacters in explicit paths must not become connection options.
	specialPath := filepath.Join(cfg.MetadataDbDir, "metadata ?&#.db")
	_, err = db.ExecContext(ctx, `VACUUM INTO ?`, specialPath)
	require.NoError(t, err)
	require.NoError(t, run([]string{"--metadata-db", specialPath, "--user-id", "owner", "grant"}, io.Discard, io.Discard))
	allowed, err := storage.IsPlatformAdmin(ctx, db, "owner")
	require.NoError(t, err)
	require.False(t, allowed, "The explicit alternate database must not change the original")
}
