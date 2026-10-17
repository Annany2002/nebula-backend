package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestAdminEndpointsRejectOrdinaryUsersAndDatabaseKeys(t *testing.T) {
	f := newBackendFixture(t)
	id, _ := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	paths := []string{"/api/v1/admin/users", "/api/v1/admin/users/regression-owner", "/api/v1/admin/stats"}
	for _, header := range []struct {
		value string
		code  int
	}{
		{"", 401}, {"Bearer invalid", 401}, {"ApiKey " + key, 401}, {"Bearer " + f.token, 403},
	} {
		for _, path := range paths {
			out := f.request("GET", path, header.value, "")
			require.Equal(t, header.code, out.Code, path+": "+out.Body.String())
			require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
			require.NotContains(t, out.Body.String(), "regression@example.test")
		}
	}
	// Client headers and a signed JWT's extra role claim cannot grant membership.
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"userId": "regression-owner", "role": "admin", "exp": time.Now().Add(time.Hour).Unix(),
	})
	forgedToken, err := forged.SignedString([]byte(f.cfg.JWTSecret))
	require.NoError(t, err)
	out := f.request("GET", paths[0], "Bearer "+forgedToken, "")
	require.Equal(t, 403, out.Code, out.Body.String())
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", true))
	for _, path := range paths {
		out = f.request("GET", path, "ApiKey "+key, "")
		require.Equal(t, 401, out.Code, "Even the admin's database API key must be rejected")
		out = f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, 200, out.Code, out.Body.String())
	}
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", false))
	for _, path := range paths {
		out = f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, 403, out.Code, "Revocation must affect the same JWT immediately")
	}
}

func TestAdminReadContractsAndPagination(t *testing.T) {
	f := newBackendFixture(t)
	ctx := context.Background()
	require.NoError(t, storage.SetPlatformAdmin(ctx, f.meta, "regression-owner", true))
	_, err := storage.CreateUser(ctx, f.meta, "another-owner", "another", "another@example.test", "private-password-hash")
	require.NoError(t, err)
	require.NoError(t, storage.RegisterDatabase(ctx, f.meta, "another-owner", "unopened", filepath.Join(f.cfg.MetadataDbDir, "unopened.db")))
	_, err = f.meta.Exec(`UPDATE users SET created_at='2026-10-17 10:00:00'`)
	require.NoError(t, err)
	out := f.request("GET", "/api/v1/admin/users?limit=1", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	var page models.AdminUserListResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &page))
	require.Len(t, page.Users, 1)
	require.EqualValues(t, 2, page.Pagination.Total)
	require.Equal(t, 1, page.Pagination.Limit)
	require.Equal(t, 0, page.Pagination.Offset)
	require.Equal(t, "another-owner", page.Users[0].UserID)
	require.EqualValues(t, 1, page.Users[0].DatabaseCount)
	assertAdminSafeFields(t, out.Body.Bytes(), true)
	out = f.request("GET", "/api/v1/admin/users/another-owner", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	assertAdminSafeFields(t, out.Body.Bytes(), false)
	out = f.request("GET", "/api/v1/admin/users?limit=1&offset=1", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code)
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &page))
	require.Equal(t, "regression-owner", page.Users[0].UserID)
	require.Equal(t, "admin", page.Users[0].Role)
	out = f.request("GET", "/api/v1/admin/users?offset=1000000", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), `"users":[]`)
	out = f.request("GET", "/api/v1/admin/stats", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code)
	require.JSONEq(t, `{"stats":{"total_users":2,"total_databases":1}}`, out.Body.String())
	require.NoFileExists(t, filepath.Join(f.cfg.MetadataDbDir, "unopened.db"))
	out = f.request("GET", "/api/v1/databases/unopened", "Bearer "+f.token, "")
	require.Equal(t, 404, out.Code, "Admin membership must not widen ordinary tenant access")
	for _, query := range []string{
		"limit=0", "limit=101", "offset=-1", "offset=1000001", "limit=", "offset=",
		"limit=1&limit=2", "offset=0&offset=1", "role=admin", "limit=1.5", "limit=%2B1",
		"limit=1e2", "limit=999999999999999999999999", "limit=%GG", "limit=1;offset=0",
	} {
		out = f.request("GET", "/api/v1/admin/users?"+query, "Bearer "+f.token, "")
		require.Equal(t, 400, out.Code, query+": "+out.Body.String())
	}
	for _, path := range []string{"/api/v1/admin/stats?limit=1", "/api/v1/admin/users/another-owner?role=admin"} {
		out = f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, 400, out.Code)
	}
	for _, id := range []string{"missing", "' OR 1=1 --"} {
		out = f.request("GET", "/api/v1/admin/users/"+url.PathEscape(id), "Bearer "+f.token, "")
		require.Equal(t, 404, out.Code, out.Body.String())
	}
}

func TestAdminReadErrorsDoNotExposeDatabaseDetails(t *testing.T) {
	f := newBackendFixture(t)
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", true))
	_, err := f.meta.Exec(`DROP TABLE databases`)
	require.NoError(t, err)
	for _, path := range []string{"/api/v1/admin/users", "/api/v1/admin/users/regression-owner", "/api/v1/admin/stats"} {
		out := f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, 500, out.Code, out.Body.String())
		require.NotContains(t, out.Body.String(), "no such table")
		require.NotContains(t, out.Body.String(), "SELECT")
	}
}

func assertAdminSafeFields(t *testing.T, body []byte, list bool) {
	t.Helper()
	var result struct {
		Users []map[string]any `json:"users"`
		User  map[string]any   `json:"user"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	users := result.Users
	if !list {
		users = []map[string]any{result.User}
	}
	for _, user := range users {
		require.Len(t, user, 8)
		for _, key := range []string{"user_id", "username", "email", "created_at", "role", "database_count", "email_verified", "verification_required"} {
			require.Contains(t, user, key)
		}
	}
}

func TestAdminGatesHonorAccountAndSessionState(t *testing.T) {
	for _, condition := range []string{"pending", "revoked", "deleted", "authorization failure"} {
		t.Run(condition, func(t *testing.T) {
			f := newBackendFixture(t)
			require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", true))
			query, code := "", 0
			switch condition {
			case "pending":
				query, code = `INSERT INTO account_email_verifications(user_id,required) VALUES('regression-owner',1)`, 403
			case "revoked":
				query, code = `INSERT INTO account_email_verifications(user_id,session_version) VALUES('regression-owner',1)`, 401
			case "deleted":
				query, code = `DELETE FROM users WHERE user_id='regression-owner'`, 401
			case "authorization failure":
				query, code = `DROP TABLE platform_admins`, 503
			}
			_, err := f.meta.Exec(query)
			require.NoError(t, err)
			out := f.request("GET", "/api/v1/admin/users", "Bearer "+f.token, "")
			require.Equal(t, code, out.Code, out.Body.String())
			require.NotContains(t, out.Body.String(), "no such table")
		})
	}
}

func TestAdminSignupAndProfileCannotGrantMembership(t *testing.T) {
	f := newBackendFixture(t)
	out := f.request("POST", "/auth/signup", "", `{"username":"attacker","email":"attacker@example.test","password":"Password123!","role":"admin","is_admin":true}`)
	require.Equal(t, 201, out.Code, out.Body.String())
	user, err := storage.FindUserByEmail(context.Background(), f.meta, "attacker@example.test")
	require.NoError(t, err)
	allowed, err := storage.IsPlatformAdmin(context.Background(), f.meta, user.UserId)
	require.NoError(t, err)
	require.False(t, allowed)
	out = f.request("PUT", "/api/v1/account/user/me", "Bearer "+f.token, `{"username":"updated","role":"admin","is_admin":true}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	allowed, err = storage.IsPlatformAdmin(context.Background(), f.meta, "regression-owner")
	require.NoError(t, err)
	require.False(t, allowed)
	token, err := auth.GenerateJWT(user.UserId, f.cfg.JWTSecret, time.Hour)
	require.NoError(t, err)
	req := httptest.NewRequest("GET", "/api/v1/admin/users", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Role", "admin")
	req.Header.Set("X-Admin", "true")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	require.Equal(t, 403, response.Code)
}
