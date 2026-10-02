package handlers_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestTelemetryDoesNotMixDatabasesWithTheSameName(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "shared")
	ctx := context.Background()
	_, err := storage.CreateUser(ctx, f.meta, "other-owner", "another", "other@example.test", "fixture-hash")
	require.NoError(t, err)
	path := filepath.Join(f.cfg.MetadataDbDir, "other.db")
	require.NoError(t, storage.RegisterDatabase(ctx, f.meta, "other-owner", "shared", path))
	otherID, err := storage.FindDatabaseIDByNameAndUser(ctx, f.meta, "other-owner", "shared")
	require.NoError(t, err)
	_, err = f.meta.Exec(`INSERT INTO database_telemetry (database_id,database_name,endpoint,method,status_code,latency_ms) VALUES
 (?, 'shared', '/api/v1/databases/:db_name/sql','POST',200,5),
 (?, 'shared', '/api/v1/databases/:db_name/sql','POST',500,9),
 (NULL, 'shared', '/api/v1/databases/:db_name/sql','POST',500,9)`, id, otherID)
	require.NoError(t, err)
	report, err := storage.GetDatabaseAnalytics(ctx, f.meta, "regression-owner", "shared")
	require.NoError(t, err)
	require.Equal(t, int64(1), report.TotalRequests)
	require.Equal(t, 100.0, report.SuccessRate)
	require.Equal(t, int64(1), report.Services[0].Requests)
	require.Equal(t, int64(0), report.Services[0].Errors)
}
