package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestAPIKeyCannotAccessAnotherDatabase(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	_, beta := f.database(t, "beta")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/databases/beta", ""},
		{"GET", "/api/v1/databases/beta/tables", ""},
		{"GET", "/api/v1/databases/beta/tables/items/records", ""},
		{"POST", "/api/v1/databases/beta/tables/items/records", `{"label":"unauthorized"}`},
		{"PUT", "/api/v1/databases/beta/tables/items/records/1", `{"label":"unauthorized"}`},
		{"DELETE", "/api/v1/databases/beta/tables/items/records/1", ""},
		{"POST", "/api/v1/databases/beta/sql", `{"query":"DELETE FROM items"}`},
		{"GET", "/api/v1/databases/beta/analytics", ""},
		{"GET", "/api/v1/databases/beta/diagram", ""},
		{"GET", "/api/v1/databases/beta/objects", ""},
		{"GET", "/api/v1/databases/beta/export/sql", ""},
		{"GET", "/api/v1/databases/beta/export/sqlite", ""},
		{"DELETE", "/api/v1/databases/beta", ""},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			out := f.request(route.method, route.path, "ApiKey "+key, route.body)
			require.Equal(t, http.StatusForbidden, out.Code, out.Body.String())
		})
	}
	var count int
	require.NoError(t, beta.QueryRow("SELECT COUNT(*) FROM items WHERE label = 'fixture'").Scan(&count))
	require.Equal(t, 1, count)
}

func TestAPIKeyOnlyAllowsScopedDataOperations(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/v1/databases", ""},
		{"POST", "/api/v1/databases", `{"db_name":"extra"}`},
		{"DELETE", "/api/v1/databases/alpha", ""},
		{"GET", "/api/v1/user/regression-owner", ""},
	} {
		out := f.request(route.method, route.path, "ApiKey "+key, route.body)
		require.Equal(t, http.StatusForbidden, out.Code, route.path+": "+out.Body.String())
	}
	out := f.request("GET", "/api/v1/databases/alpha/tables/items/records", "ApiKey "+key, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	out = f.request("GET", "/api/v1/databases", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
}

func TestUnsupportedAuthorizationSchemeReturnsUnauthorized(t *testing.T) {
	f := newBackendFixture(t)
	out := f.request("GET", "/api/v1/databases", "Unsupported fixture", "")
	require.Equal(t, http.StatusUnauthorized, out.Code, out.Body.String())
	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &payload))
	require.NotEmpty(t, payload["error"])
}

func TestAPIKeysAreHashedAndOnlyDisclosedOnCreation(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	out := f.request("POST", "/api/v1/account/databases/alpha/apikey", "Bearer "+f.token, "")
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var created struct {
		Key string `json:"api_key"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &created))
	require.NotEmpty(t, created.Key)
	var stored string
	require.NoError(t, f.meta.QueryRow("SELECT key FROM api_keys WHERE api_database_id = ?", id).Scan(&stored))
	require.NotEqual(t, created.Key, stored, "metadata must not store usable API secrets")
	for _, path := range []string{"/api/v1/account/databases/alpha/apikey", "/api/v1/databases", "/api/v1/databases/alpha"} {
		out = f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, http.StatusOK, out.Code, out.Body.String())
		require.NotContains(t, out.Body.String(), created.Key, path)
	}
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+created.Key, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
}

func TestAPIKeyRotationRevokesPreviousKey(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	oldKey, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	out := f.request("POST", "/api/v1/account/databases/alpha/apikey", "Bearer "+f.token, "")
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var created struct {
		Key string `json:"api_key"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &created))
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+oldKey, "")
	require.Equal(t, http.StatusUnauthorized, out.Code)
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+created.Key, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	out = f.request("DELETE", "/api/v1/account/databases/alpha/apikey", "Bearer "+f.token, "")
	require.Equal(t, http.StatusNoContent, out.Code, out.Body.String())
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+created.Key, "")
	require.Equal(t, http.StatusUnauthorized, out.Code)
}

func TestLegacyAPIKeysAreMigratedWithoutRevocation(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	legacy := "neb_legacy_regression_secret"
	// Reproduce the original schema, before display prefixes were introduced.
	_, err := f.meta.Exec("ALTER TABLE api_keys DROP COLUMN key_prefix")
	require.NoError(t, err)
	_, err = f.meta.Exec("INSERT INTO api_keys (api_owner_id,api_database_id,key) VALUES (?,?,?)", "regression-owner", id, legacy)
	require.NoError(t, err)
	require.NoError(t, f.meta.Close())
	reopened, err := storage.ConnectMetadataDB(f.cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	f.meta = reopened
	f.router = api.SetupRouter(reopened, f.cfg)
	var stored string
	require.NoError(t, reopened.QueryRow("SELECT key FROM api_keys WHERE api_database_id = ?", id).Scan(&stored))
	require.NotEqual(t, legacy, stored)
	owner, migratedID, err := storage.AuthenticateAPIKey(context.Background(), reopened, legacy)
	require.NoError(t, err)
	require.Equal(t, "regression-owner", owner)
	require.Equal(t, id, migratedID)
	metadata, err := storage.FindAPIKeyMetadata(context.Background(), reopened, id)
	require.NoError(t, err)
	require.NotEmpty(t, metadata.KeyPrefix)
	require.NotContains(t, metadata.KeyPrefix, legacy)
	require.NoError(t, reopened.Close())
	reopenedAgain, err := storage.ConnectMetadataDB(f.cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopenedAgain.Close()) })
	owner, migratedID, err = storage.AuthenticateAPIKey(context.Background(), reopenedAgain, legacy)
	require.NoError(t, err)
	require.Equal(t, "regression-owner", owner)
	require.Equal(t, id, migratedID)
}

func TestUserResponsesDoNotExposePasswordHashes(t *testing.T) {
	f := newBackendFixture(t)
	hash, err := auth.HashPassword("RegressionPassword123!")
	require.NoError(t, err)
	_, err = f.meta.Exec("UPDATE users SET password_hash=? WHERE user_id='regression-owner'", hash)
	require.NoError(t, err)
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/auth/login", `{"email":"regression@example.test","password":"RegressionPassword123!"}`},
		{"GET", "/api/v1/user/regression-owner", ""},
	} {
		out := f.request(route.method, route.path, "Bearer "+f.token, route.body)
		require.Equal(t, http.StatusOK, out.Code, out.Body.String())
		require.NotContains(t, out.Body.String(), hash)
		require.NotContains(t, out.Body.String(), `"password"`)
	}
}

func TestSQLCannotAttachAnotherDatabase(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	_, beta := f.database(t, "beta")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	query := "ATTACH DATABASE '" + filepath.Join(f.cfg.MetadataDbDir, "beta.db") + "' AS victim; DELETE FROM victim.items;"
	out := f.request("POST", "/api/v1/databases/alpha/sql", "ApiKey "+key, fixtureJSON(t, map[string]string{"query": query}))
	require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	var count int
	require.NoError(t, beta.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
}
