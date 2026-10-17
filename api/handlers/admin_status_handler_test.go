package handlers_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func adminStatusFixture(t *testing.T) (*backendFixture, string, string) {
	t.Helper()
	f := newBackendFixture(t)
	hash, err := auth.HashPassword("Password123!")
	require.NoError(t, err)
	_, err = f.meta.Exec(`UPDATE users SET password_hash=? WHERE user_id='regression-owner'`, hash)
	require.NoError(t, err)
	_, err = storage.CreateUser(context.Background(), f.meta, "operator", "operator", "operator@example.test", hash)
	require.NoError(t, err)
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "operator", true))
	token, err := auth.GenerateJWT("operator", f.cfg.JWTSecret, time.Hour)
	require.NoError(t, err)
	id, _ := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	return f, token, key
}

func TestAdminSuspensionBlocksLoginExistingJWTsAndKeysUntilActivation(t *testing.T) {
	f, operator, key := adminStatusFixture(t)
	stats := f.request("GET", "/api/v1/admin/stats", "Bearer "+operator, "")
	require.Equal(t, 200, stats.Code, stats.Body.String())
	require.JSONEq(t, `{"stats":{"total_users":2,"total_databases":1,"total_tables":1,"databases_counted":1,"databases_unavailable":0}}`, stats.Body.String())
	base := "/api/v1/admin/users/regression-owner/"
	for range 2 {
		out := f.request("PUT", base+"suspend", "Bearer "+operator, "")
		require.Equal(t, 200, out.Code, out.Body.String())
		require.Contains(t, out.Body.String(), `"status":"suspended"`)
	}
	for _, credential := range []string{"Bearer " + f.token, "ApiKey " + key} {
		for _, route := range []struct{ method, path, body string }{
			{"GET", "/api/v1/databases/alpha/tables/items/records", ""},
			{"POST", "/api/v1/databases/alpha/sql", `{"query":"DELETE FROM items"}`},
			{"POST", "/api/v1/databases/alpha/tables/items/records", `{"label":"forbidden"}`},
		} {
			out := f.request(route.method, route.path, credential, route.body)
			require.Equal(t, 403, out.Code, out.Body.String())
			require.Contains(t, out.Body.String(), "account_suspended")
		}
	}
	for _, path := range []string{"/api/v1/account/user/me", "/api/v1/databases", "/api/v1/backups", "/api/v1/admin/users"} {
		out := f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, 403, out.Code)
		require.Contains(t, out.Body.String(), "account_suspended")
	}
	out := f.request("POST", "/auth/login", "", `{"email":"regression@example.test","password":"Password123!"}`)
	require.Equal(t, 403, out.Code)
	require.NotContains(t, out.Body.String(), `"token"`)
	for range 2 {
		out = f.request("PUT", base+"activate", "Bearer "+operator, `{}`)
		require.Equal(t, 200, out.Code, out.Body.String())
		require.Contains(t, out.Body.String(), `"status":"active"`)
	}
	out = f.request("GET", "/api/v1/databases", "Bearer "+f.token, "")
	require.Equal(t, 401, out.Code)
	require.Contains(t, out.Body.String(), "session_revoked")
	out = f.request("GET", "/api/v1/databases/alpha/tables/items/records", "ApiKey "+key, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Contains(t, out.Body.String(), "fixture")
	out = f.request("POST", "/auth/login", "", `{"email":"regression@example.test","password":"Password123!"}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	var login struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &login))
	out = f.request("GET", "/api/v1/databases", "Bearer "+login.Token, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	var events, version int
	require.NoError(t, f.meta.QueryRow(`SELECT COUNT(*) FROM platform_account_events WHERE target_user_id='regression-owner'`).Scan(&events))
	require.Equal(t, 2, events)
	require.NoError(t, f.meta.QueryRow(`SELECT session_version FROM account_email_verifications WHERE user_id='regression-owner'`).Scan(&version))
	require.Equal(t, 2, version)
}

func TestAdminStatusPermissionsValidationAndProtectedAccounts(t *testing.T) {
	f, operator, key := adminStatusFixture(t)
	base := "/api/v1/admin/users/regression-owner/"
	for _, action := range []string{"suspend", "activate"} {
		for _, header := range []struct {
			value string
			code  int
		}{{"", 401}, {"Bearer " + f.token, 403}, {"ApiKey " + key, 401}} {
			out := f.request("PUT", base+action, header.value, "")
			require.Equal(t, header.code, out.Code, out.Body.String())
			require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		}
		out := f.request("PUT", "/api/v1/admin/users/missing/"+action, "Bearer "+operator, "")
		require.Equal(t, 404, out.Code)
	}
	out := f.request("PUT", "/api/v1/admin/users/operator/suspend", "Bearer "+operator, "")
	require.Equal(t, 409, out.Code)
	require.Contains(t, out.Body.String(), "admin_account_protected")
	for _, body := range []string{`null`, `[]`, `{"role":"admin"}`, `{"status":"active"}`, `{} {}`, `{`} {
		out = f.request("PUT", base+"suspend", "Bearer "+operator, body)
		require.Equal(t, 400, out.Code, body+": "+out.Body.String())
	}
	out = f.request("PUT", base+"suspend", "Bearer "+operator, strings.Repeat(" ", 4097))
	require.Equal(t, 413, out.Code)
	out = f.request("PUT", base+"suspend?user_id=operator", "Bearer "+operator, "")
	require.Equal(t, 400, out.Code)
	access, err := storage.GetEmailVerificationStatus(context.Background(), f.meta, "regression-owner")
	require.NoError(t, err)
	require.False(t, access.Suspended)
	require.Zero(t, access.SessionVersion)
	// Protected users become eligible only after an explicit, trusted role revocation.
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", true))
	out = f.request("PUT", base+"suspend", "Bearer "+operator, "")
	require.Equal(t, 409, out.Code)
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", false))
	out = f.request("PUT", base+"suspend", "Bearer "+operator, "")
	require.Equal(t, 200, out.Code, out.Body.String())
}

func TestAdminActivationDoesNotBypassEmailVerification(t *testing.T) {
	f, operator, key := adminStatusFixture(t)
	_, err := f.meta.Exec(`INSERT INTO account_email_verifications(user_id,required) VALUES('regression-owner',1)`)
	require.NoError(t, err)
	for _, action := range []string{"suspend", "activate"} {
		out := f.request("PUT", "/api/v1/admin/users/regression-owner/"+action, "Bearer "+operator, "")
		require.Equal(t, 200, out.Code, out.Body.String())
	}
	out := f.request("POST", "/auth/login", "", `{"email":"regression@example.test","password":"Password123!"}`)
	require.Equal(t, 403, out.Code)
	require.Contains(t, out.Body.String(), "email_verification_required")
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+key, "")
	require.Equal(t, 403, out.Code)
	require.Contains(t, out.Body.String(), "email_verification_required")
}
