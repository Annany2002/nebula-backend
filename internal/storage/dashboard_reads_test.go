package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminUserFiltersAreLiteralCombinedAndPaginated(t *testing.T) {
	db, _ := accountStatusFixture(t)
	ctx := context.Background()
	_, err := CreateUser(ctx, db, "literal", `build_%\team`, "literal@example.test", "private")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE users SET username='BuildTarget' WHERE user_id='target'; UPDATE users SET created_at='2026-10-17 10:00:00'`)
	require.NoError(t, err)
	_, err = SetAccountSuspended(ctx, db, "operator", "target", 0, true)
	require.NoError(t, err)
	for _, test := range []struct {
		filter AdminUserFilter
		ids    []string
	}{
		{AdminUserFilter{Query: "BUILD"}, []string{"literal", "target"}},
		{AdminUserFilter{Query: "%"}, []string{"literal"}},
		{AdminUserFilter{Query: "_"}, []string{"literal"}},
		{AdminUserFilter{Query: `\`}, []string{"literal"}},
		{AdminUserFilter{Query: " OPERATOR@EXAMPLE.TEST "}, []string{"operator"}},
		{AdminUserFilter{Role: "admin"}, []string{"operator"}},
		{AdminUserFilter{Role: "user", Status: "active", Query: "build"}, []string{"literal"}},
		{AdminUserFilter{Status: "suspended"}, []string{"target"}},
		{AdminUserFilter{Role: "admin", Status: "suspended"}, []string{}},
		{AdminUserFilter{Query: "' OR 1=1 --"}, []string{}},
	} {
		users, total, err := ListAdminUsers(ctx, db, 20, 0, test.filter)
		require.NoError(t, err)
		require.EqualValues(t, len(test.ids), total)
		ids := make([]string, 0, len(users))
		for _, user := range users {
			ids = append(ids, user.UserID)
		}
		require.Equal(t, test.ids, ids)
	}
	users, total, err := ListAdminUsers(ctx, db, 1, 1, AdminUserFilter{Query: "build"})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, users, 1)
	require.Equal(t, "target", users[0].UserID)
	for _, filter := range []AdminUserFilter{{Query: strings.Repeat("a", 101)}, {Query: "bad\nvalue"}, {Query: string([]byte{255})}, {Role: "owner"}, {Status: "disabled"}} {
		_, _, err := ListAdminUsers(ctx, db, 20, 0, filter)
		require.ErrorIs(t, err, ErrInvalidAdminQuery)
	}
}

func TestAdminAuditHistoryIsFilteredStableAndSurvivesDeletedAccounts(t *testing.T) {
	db, _ := accountStatusFixture(t)
	ctx := context.Background()
	events, total, err := ListAdminAuditEvents(ctx, db, 20, 0, AdminAuditFilter{})
	require.NoError(t, err)
	require.Zero(t, total)
	require.NotNil(t, events)
	require.Empty(t, events)
	for _, suspended := range []bool{true, true, false, false} {
		_, err := SetAccountSuspended(ctx, db, "operator", "target", 0, suspended)
		require.NoError(t, err)
	}
	events, total, err = ListAdminAuditEvents(ctx, db, 1, 0, AdminAuditFilter{ActorUserID: "operator", TargetUserID: "target"})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, events, 1)
	require.Equal(t, "activate", events[0].Action)
	latest := events[0].EventID
	events, total, err = ListAdminAuditEvents(ctx, db, 1, 1, AdminAuditFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Equal(t, "suspend", events[0].Action)
	require.Less(t, events[0].EventID, latest)
	events, total, err = ListAdminAuditEvents(ctx, db, 20, 0, AdminAuditFilter{Action: "suspend"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, events, 1)
	_, err = db.Exec(`DELETE FROM users WHERE user_id IN ('target','operator')`)
	require.NoError(t, err)
	events, total, err = ListAdminAuditEvents(ctx, db, 20, 0, AdminAuditFilter{ActorUserID: "operator", TargetUserID: "target"})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, events, 2)
	for _, filter := range []AdminAuditFilter{{Action: "delete"}, {TargetUserID: strings.Repeat("a", 129)}, {ActorUserID: "bad\x00id"}} {
		_, _, err := ListAdminAuditEvents(ctx, db, 20, 0, filter)
		require.ErrorIs(t, err, ErrInvalidAdminQuery)
	}
	events, total, err = ListAdminAuditEvents(ctx, db, 20, 0, AdminAuditFilter{TargetUserID: "' OR 1=1 --"})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, events)
	events, total, err = ListAdminAuditEvents(ctx, db, 20, 1000000, AdminAuditFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Empty(t, events)
	require.NotNil(t, events)
	for _, page := range [][2]int{{0, 0}, {101, 0}, {20, -1}, {20, 1000001}} {
		_, _, err := ListAdminAuditEvents(ctx, db, page[0], page[1], AdminAuditFilter{})
		require.ErrorIs(t, err, ErrInvalidAdminQuery)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = ListAdminAuditEvents(cancelled, db, 20, 0, AdminAuditFilter{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestAccountSummaryIsScopedAndCountsOnlyReadyBackupMetadata(t *testing.T) {
	db, cfg := accountStatusFixture(t)
	ctx := context.Background()
	summary, err := GetAccountSummary(ctx, db, "target")
	require.NoError(t, err)
	require.Zero(t, summary.DatabaseCount)
	require.Zero(t, summary.ActiveAPIKeyCount)
	require.Zero(t, summary.ReadyBackupCount)
	missing := filepath.Join(cfg.MetadataDbDir, "never-open.db")
	require.NoError(t, RegisterDatabase(ctx, db, "target", "owned", missing))
	require.NoError(t, RegisterDatabase(ctx, db, "operator", "other", filepath.Join(cfg.MetadataDbDir, "other.db")))
	id, err := FindDatabaseIDByNameAndUser(ctx, db, "target", "owned")
	require.NoError(t, err)
	_, err = StoreAPIKey(ctx, db, "target", id)
	require.NoError(t, err)
	for _, status := range []string{"ready", "creating", "deleting", "deleted"} {
		_, err := db.Exec(`INSERT INTO database_backups(backup_id,owner_id,source_database_id,db_name,status) VALUES(?,?,?,?,?)`, status, "target", id, "owned", status)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO database_backups(backup_id,owner_id,source_database_id,db_name,status) VALUES('other-ready','operator',?,'other','ready')`, id)
	require.NoError(t, err)
	summary, err = GetAccountSummary(ctx, db, "target")
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.DatabaseCount)
	require.EqualValues(t, 1, summary.ActiveAPIKeyCount)
	require.EqualValues(t, 1, summary.ReadyBackupCount)
	require.NoFileExists(t, missing)
	summary, err = GetAccountSummary(ctx, db, "operator")
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.DatabaseCount)
	require.Zero(t, summary.ActiveAPIKeyCount)
	require.EqualValues(t, 1, summary.ReadyBackupCount)
	// Snapshots outlive a deleted source; its active API key does not.
	_, err = db.Exec(`DELETE FROM databases WHERE database_id=?`, id)
	require.NoError(t, err)
	summary, err = GetAccountSummary(ctx, db, "target")
	require.NoError(t, err)
	require.Zero(t, summary.DatabaseCount)
	require.Zero(t, summary.ActiveAPIKeyCount)
	require.EqualValues(t, 1, summary.ReadyBackupCount)
	_, err = GetAccountSummary(ctx, db, "' OR 1=1 --")
	require.ErrorIs(t, err, ErrUserNotFound)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = GetAccountSummary(cancelled, db, "target")
	require.ErrorIs(t, err, context.Canceled)
}
