package handlers_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestProfileRoleUsesCurrentMembershipAndCannotBeChangedThroughProfile(t *testing.T) {
	f := newBackendFixture(t)
	for _, admin := range []bool{false, true, false} {
		require.NoError(t, storage.SetPlatformAdmin(context.Background(), f.meta, "regression-owner", admin))
		out := f.request("GET", "/api/v1/account/user/me", "Bearer "+f.token, "")
		require.Equal(t, 200, out.Code, out.Body.String())
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		var profile models.UserProfileResponse
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &profile))
		role := "user"
		if admin {
			role = "admin"
		}
		require.Equal(t, role, profile.Role)
	}
	out := f.request("PUT", "/api/v1/account/user/me", "Bearer "+f.token, `{"username":"builder","role":"admin"}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	var response struct {
		User models.UserProfileResponse `json:"user"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
	require.Equal(t, "user", response.User.Role)
}

func TestAdminFilterAndAuditHTTPContractsAndAuthorization(t *testing.T) {
	f, operator, key := adminStatusFixture(t)
	usersPath := "/api/v1/admin/users?q=regression&role=user&status=active"
	out := f.request("GET", usersPath, "Bearer "+operator, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	var users models.AdminUserListResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &users))
	require.EqualValues(t, 1, users.Pagination.Total)
	for _, action := range []string{"suspend", "activate"} {
		out = f.request("PUT", "/api/v1/admin/users/regression-owner/"+action, "Bearer "+operator, "")
		require.Equal(t, 200, out.Code, out.Body.String())
	}
	path := "/api/v1/admin/audit-events?target_user_id=regression-owner&actor_user_id=operator&limit=1"
	for _, header := range []struct {
		value string
		code  int
	}{{"", 401}, {"ApiKey " + key, 401}} {
		out = f.request("GET", path, header.value, "")
		require.Equal(t, header.code, out.Code)
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	}
	// The ordinary user's JWT is revoked by the transition; a fresh version is still not an admin.
	fresh, err := storage.FindUserByUserId(context.Background(), f.meta, "regression-owner")
	require.NoError(t, err)
	ordinary, err := auth.GenerateSessionJWT("regression-owner", f.cfg.JWTSecret, time.Hour, fresh.SessionVersion)
	require.NoError(t, err)
	out = f.request("GET", path, "Bearer "+ordinary, "")
	require.Equal(t, 403, out.Code)
	out = f.request("GET", path, "Bearer "+operator, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	var page models.AdminAuditListResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &page))
	require.EqualValues(t, 2, page.Pagination.Total)
	require.Len(t, page.Events, 1)
	require.Equal(t, "activate", page.Events[0].Action)
	var raw struct {
		Events []map[string]any `json:"events"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &raw))
	require.Len(t, raw.Events[0], 5)
	for _, field := range []string{"event_id", "actor_user_id", "target_user_id", "action", "created_at"} {
		require.Contains(t, raw.Events[0], field)
	}
	for _, query := range []string{"limit=0", "offset=1000001", "action=delete", "action=", "target_user_id=", "actor_user_id=x&actor_user_id=y", "q=private", "limit=%GG", "target_user_id=" + strings.Repeat("x", 129)} {
		out = f.request("GET", "/api/v1/admin/audit-events?"+query, "Bearer "+operator, "")
		require.Equal(t, 400, out.Code, query+": "+out.Body.String())
	}
	for _, query := range []string{"q=", "q=x&q=y", "role=owner", "role=", "status=disabled", "q=" + url.QueryEscape(strings.Repeat("x", 101)), "q=%FF", "q=%0Afoo"} {
		out = f.request("GET", "/api/v1/admin/users?"+query, "Bearer "+operator, "")
		require.Equal(t, 400, out.Code, query+": "+out.Body.String())
	}
	out = f.request("GET", "/api/v1/admin/audit-events?action=suspend&offset=1000000", "Bearer "+operator, "")
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), `"events":[]`)
}

func TestAccountSummaryHTTPIsOwnerOnlyAndNeverLeaksCredentials(t *testing.T) {
	f, operator, key := adminStatusFixture(t)
	path := "/api/v1/account/summary"
	for _, header := range []string{"", "ApiKey " + key} {
		out := f.request("GET", path, header, "")
		require.Equal(t, 401, out.Code)
	}
	out := f.request("GET", path, "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"summary":{"database_count":1,"active_api_key_count":1,"ready_backup_count":0}}`, out.Body.String())
	out = f.request("GET", path, "Bearer "+operator, "")
	require.Equal(t, 200, out.Code)
	require.JSONEq(t, `{"summary":{"database_count":0,"active_api_key_count":0,"ready_backup_count":0}}`, out.Body.String())
	out = f.request("GET", path+"?user_id=regression-owner", "Bearer "+operator, "")
	require.Equal(t, 400, out.Code)
	_, err := f.meta.Exec(`INSERT INTO account_suspensions(user_id) VALUES('regression-owner')`)
	require.NoError(t, err)
	out = f.request("GET", path, "Bearer "+f.token, "")
	require.Equal(t, 403, out.Code)
	require.Contains(t, out.Body.String(), "account_suspended")
}
