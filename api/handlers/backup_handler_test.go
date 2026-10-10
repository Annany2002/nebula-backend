package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func createFixtureBackup(t *testing.T, f *backendFixture, database string) models.BackupResponse {
	t.Helper()
	id := uuid.NewString()
	response := f.request("POST", "/api/v1/databases/"+database+"/backups", "Bearer "+f.token, fixtureJSON(t, models.CreateBackupRequest{BackupID: id}))
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	require.Equal(t, "/api/v1/backups/"+id, response.Header().Get("Location"))
	var backup models.BackupResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &backup))
	require.Equal(t, id, backup.Backup.BackupID)
	return backup
}

func TestBackupHTTPContractAndRestoreAfterSourceDeletion(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "backup_source")
	listed := f.request("GET", "/api/v1/databases/backup_source/tables", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	backup := createFixtureBackup(t, f, "backup_source")
	id := backup.Backup.BackupID
	authorization := "Bearer " + f.token
	response := f.request("POST", "/api/v1/databases/backup_source/backups", authorization, fixtureJSON(t, models.CreateBackupRequest{BackupID: id}))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var replay models.BackupResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &replay))
	require.Equal(t, backup, replay)
	response = f.request("GET", "/api/v1/backups?db_name=backup_source&limit=1&offset=0", authorization, "")
	require.Equal(t, http.StatusOK, response.Code)
	var page struct {
		Backups    []json.RawMessage      `json:"backups"`
		Pagination storage.PaginationMeta `json:"pagination"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Backups, 1)
	require.Equal(t, 1, page.Pagination.Total)
	require.NotContains(t, response.Body.String(), "file_path")
	require.NotContains(t, response.Body.String(), "owner_id")
	response = f.request("GET", "/api/v1/backups/"+id+"/download", authorization, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	sum := sha256.Sum256(response.Body.Bytes())
	require.Equal(t, backup.Backup.SHA256, hex.EncodeToString(sum[:]))
	require.Equal(t, backup.Backup.SizeBytes, int64(response.Body.Len()))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Contains(t, response.Header().Get("Content-Disposition"), id+".db")
	_, err := source.Exec("INSERT INTO items VALUES(2,'after-backup')")
	require.NoError(t, err)
	response = f.request("POST", "/api/v1/backups/"+id+"/restore", authorization, `{"db_name":"backup_source"}`)
	require.Equal(t, http.StatusConflict, response.Code)
	require.NoError(t, source.Close())
	response = f.request("DELETE", "/api/v1/databases/backup_source", authorization, "")
	require.Equal(t, http.StatusNoContent, response.Code)
	response = f.request("GET", "/api/v1/backups/"+id, authorization, "")
	require.Equal(t, http.StatusOK, response.Code)
	response = f.request("POST", "/api/v1/backups/"+id+"/restore", authorization, `{"db_name":"restored_from_backup"}`)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var restored models.RestoreBackupResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &restored))
	require.Equal(t, id, restored.BackupID)
	require.Equal(t, backup.Backup.SizeBytes, restored.SizeBytes)
	path, err := storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", restored.DBName)
	require.NoError(t, err)
	db, err := storage.ConnectUserDB(t.Context(), path)
	require.NoError(t, err)
	defer db.Close()
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
	response = f.request("POST", "/api/v1/backups/"+id+"/restore", authorization, `{"db_name":"restored_from_backup"}`)
	require.Equal(t, http.StatusConflict, response.Code)
	for range 2 {
		response = f.request("DELETE", "/api/v1/backups/"+id, authorization, "")
		require.Equal(t, http.StatusNoContent, response.Code)
	}
	response = f.request("GET", "/api/v1/backups/"+id, authorization, "")
	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestBackupImmediatelyAfterNativeDatabaseCreation(t *testing.T) {
	f := newBackendFixture(t)
	response := f.request("POST", "/api/v1/databases", "Bearer "+f.token, `{"db_name":"untouched"}`)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	backup := createFixtureBackup(t, f, "untouched")
	require.GreaterOrEqual(t, backup.Backup.SizeBytes, int64(512))
	response = f.request("POST", "/api/v1/backups/"+backup.Backup.BackupID+"/restore", "Bearer "+f.token, `{"db_name":"untouched_copy"}`)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
}

func TestBackupHTTPRejectsKeysAndOtherOwners(t *testing.T) {
	f := newBackendFixture(t)
	dbID, _ := f.database(t, "private_source")
	backup := createFixtureBackup(t, f, "private_source")
	id := backup.Backup.BackupID
	key, err := storage.StoreAPIKey(t.Context(), f.meta, "regression-owner", dbID)
	require.NoError(t, err)
	_, err = storage.CreateUser(t.Context(), f.meta, "other-owner", "other_user", "backup-other@example.test", "test-hash")
	require.NoError(t, err)
	token, err := auth.GenerateJWT("other-owner", f.cfg.JWTSecret, f.cfg.JWTExpiration)
	require.NoError(t, err)
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/v1/databases/private_source/backups", fixtureJSON(t, models.CreateBackupRequest{BackupID: uuid.NewString()})},
		{"GET", "/api/v1/backups", ""}, {"GET", "/api/v1/backups/" + id, ""}, {"GET", "/api/v1/backups/" + id + "/download", ""}, {"POST", "/api/v1/backups/" + id + "/restore", `{"db_name":"stolen"}`}, {"DELETE", "/api/v1/backups/" + id, ""},
	} {
		response := f.request(route.method, route.path, "ApiKey "+key, route.body)
		require.Equal(t, http.StatusForbidden, response.Code, route.path)
		response = f.request(route.method, route.path, "", route.body)
		require.Equal(t, http.StatusUnauthorized, response.Code, route.path)
	}
	for _, method := range []string{"GET", "DELETE"} {
		response := f.request(method, "/api/v1/backups/"+id, "Bearer "+token, "")
		expected := http.StatusNotFound
		if method == "DELETE" {
			expected = http.StatusNoContent
		}
		require.Equal(t, expected, response.Code)
	}
	response := f.request("POST", "/api/v1/backups/"+id+"/restore", "Bearer "+token, `{"db_name":"stolen"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
	response = f.request("GET", "/api/v1/backups", "Bearer "+token, "")
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"backups":[]`)
	response = f.request("GET", "/api/v1/backups/"+id, "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, response.Code)
}

func TestBackupHTTPValidationAndMissingResources(t *testing.T) {
	f := newBackendFixture(t)
	f.database(t, "validation_source")
	authorization := "Bearer " + f.token
	for _, body := range []string{"", "null", "{}", `{"backup_id":"../escape"}`, `{"backup_id":"` + uuid.NewString() + `","owner_id":"attacker"}`, `{"backup_id":"` + uuid.NewString() + `"} {}`} {
		response := f.request("POST", "/api/v1/databases/validation_source/backups", authorization, body)
		require.Equal(t, http.StatusBadRequest, response.Code, body)
	}
	oversized := `{"backup_id":"` + strings.Repeat("x", 5000) + `"}`
	response := f.request("POST", "/api/v1/databases/validation_source/backups", authorization, oversized)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	req := httptest.NewRequest("POST", "/api/v1/databases/validation_source/backups", strings.NewReader("{}"))
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "text/plain")
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, http.StatusUnsupportedMediaType, out.Code)
	for _, query := range []string{"limit=0", "limit=101", "limit=one", "limit=", "offset=-1", "offset=1000001", "limit=1&limit=2", "unknown=x", "db_name=../invalid", "limit=%ZZ"} {
		response = f.request("GET", "/api/v1/backups?"+query, authorization, "")
		require.Equal(t, http.StatusBadRequest, response.Code, query)
	}
	response = f.request("POST", "/api/v1/databases/missing/backups", authorization, fixtureJSON(t, models.CreateBackupRequest{BackupID: uuid.NewString()}))
	require.Equal(t, http.StatusNotFound, response.Code)
	response = f.request("GET", "/api/v1/backups/not-a-uuid", authorization, "")
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = f.request("GET", "/api/v1/backups/"+uuid.NewString(), authorization, "")
	require.Equal(t, http.StatusNotFound, response.Code)
	response = f.request("GET", "/api/v1/backups", authorization, "")
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"backups":[]`)
}

func TestBackupHTTPDownloadAndRestoreRejectCorruption(t *testing.T) {
	f := newBackendFixture(t)
	f.database(t, "corrupt_source")
	backup := createFixtureBackup(t, f, "corrupt_source")
	id := backup.Backup.BackupID
	path := filepath.Join(f.cfg.MetadataDbDir, ".nebula-backups", "regression-owner", id+".db")
	require.NoError(t, os.WriteFile(path, []byte("broken"), 0o600))
	response := f.request("GET", "/api/v1/backups/"+id+"/download", "Bearer "+f.token, "")
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "backup_unavailable")
	response = f.request("POST", "/api/v1/backups/"+id+"/restore", "Bearer "+f.token, `{"db_name":"should_not_exist"}`)
	require.Equal(t, http.StatusConflict, response.Code)
	_, err := storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", "should_not_exist")
	require.ErrorIs(t, err, storage.ErrDatabaseNotFound)
}

func TestBackupHTTPKeepAliveAfterJSONRejection(t *testing.T) {
	f := newBackendFixture(t)
	f.database(t, "keepalive_source")
	server := httptest.NewServer(f.router)
	defer server.Close()
	client := server.Client()
	for _, body := range []string{"{", fixtureJSON(t, models.CreateBackupRequest{BackupID: uuid.NewString()}), fixtureJSON(t, models.CreateBackupRequest{BackupID: uuid.NewString()})} {
		req, err := http.NewRequest("POST", server.URL+"/api/v1/databases/keepalive_source/backups", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+f.token)
		response, err := client.Do(req)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		expected := http.StatusCreated
		if body == "{" {
			expected = http.StatusBadRequest
			require.True(t, response.Close)
		}
		require.Equal(t, expected, response.StatusCode, string(data))
	}
}

func TestDatabaseDeletionWaitsForSnapshotLifecycleAndCanCancel(t *testing.T) {
	f := newBackendFixture(t)
	f.database(t, "locked_source")
	release, err := storage.LockDatabaseLifecycle(t.Context(), "regression-owner", "locked_source")
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("DELETE", "/api/v1/databases/locked_source", http.NoBody).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+f.token)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	require.Equal(t, http.StatusRequestTimeout, response.Code)
	_, err = storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", "locked_source")
	require.NoError(t, err)
	release()
	response = f.request("DELETE", "/api/v1/databases/locked_source", "Bearer "+f.token, "")
	require.Equal(t, http.StatusNoContent, response.Code)
}
