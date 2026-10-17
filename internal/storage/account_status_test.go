package storage

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/domain"
)

func accountStatusFixture(t *testing.T) (*sql.DB, *config.Config) {
	t.Helper()
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	db, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, id := range []string{"operator", "target"} {
		_, err = CreateUser(context.Background(), db, id, id, id+"@example.test", "private-hash")
		require.NoError(t, err)
	}
	require.NoError(t, SetPlatformAdmin(context.Background(), db, "operator", true))
	return db, cfg
}

func TestAccountSuspensionIsAtomicIdempotentPersistentAndAudited(t *testing.T) {
	db, cfg := accountStatusFixture(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO account_email_verifications(user_id,verified_email,pending_email,required) VALUES('target','target@example.test','pending@example.test',1)`)
	require.NoError(t, err)
	for _, suspended := range []bool{true, false} {
		var workers sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				_, err := SetAccountSuspended(ctx, db, "operator", "target", 0, suspended)
				results <- err
			}()
		}
		workers.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		access, err := GetEmailVerificationStatus(ctx, db, "target")
		require.NoError(t, err)
		require.Equal(t, suspended, access.Suspended)
		require.True(t, access.Verified)
		require.True(t, access.Required)
		require.Equal(t, "pending@example.test", access.PendingEmail)
		expected := 2
		if suspended {
			expected = 1
		}
		require.Equal(t, expected, access.SessionVersion)
		var events int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM platform_account_events WHERE actor_user_id='operator' AND target_user_id='target'`).Scan(&events))
		require.Equal(t, expected, events)
	}
	_, err = SetAccountSuspended(ctx, db, "operator", "target", 0, true)
	require.NoError(t, err)
	require.ErrorIs(t, SetPlatformAdmin(ctx, db, "target", true), ErrAccountSuspended)
	allowed, err := IsPlatformAdmin(ctx, db, "target")
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, db.Close())
	reopened, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer reopened.Close()
	user, err := GetAdminUser(ctx, reopened, "target")
	require.NoError(t, err)
	require.Equal(t, domain.AccountSuspended, user.Status)
	access, err := GetEmailVerificationStatus(ctx, reopened, "target")
	require.NoError(t, err)
	require.Equal(t, 3, access.SessionVersion)
}

func TestAccountStatusRechecksActorsProtectsAdminsAndRollsBackFailures(t *testing.T) {
	db, _ := accountStatusFixture(t)
	ctx := context.Background()
	for _, test := range []struct {
		actor, target string
		version       int
		want          error
	}{
		{"target", "operator", 0, ErrAdminRequired},
		{"operator", "operator", 0, ErrAdminProtected},
		{"operator", "missing", 0, ErrUserNotFound},
		{"operator", "target", 1, ErrAdminSessionRevoked},
	} {
		_, err := SetAccountSuspended(ctx, db, test.actor, test.target, test.version, true)
		require.ErrorIs(t, err, test.want)
	}
	_, err := db.Exec(`CREATE TRIGGER fail_admin_audit BEFORE INSERT ON platform_account_events BEGIN SELECT RAISE(ABORT,'fixture audit failure'); END`)
	require.NoError(t, err)
	_, err = SetAccountSuspended(ctx, db, "operator", "target", 0, true)
	require.Error(t, err)
	access, err := GetEmailVerificationStatus(ctx, db, "target")
	require.NoError(t, err)
	require.False(t, access.Suspended)
	require.Zero(t, access.SessionVersion)
	_, err = db.Exec(`DROP TRIGGER fail_admin_audit; INSERT INTO account_suspensions(user_id) VALUES('operator')`)
	require.NoError(t, err)
	_, err = SetAccountSuspended(ctx, db, "operator", "target", 0, true)
	require.ErrorIs(t, err, ErrAdminRequired)
	_, err = db.Exec(`DELETE FROM account_suspensions; INSERT INTO account_email_verifications(user_id,required) VALUES('operator',1)`)
	require.NoError(t, err)
	_, err = SetAccountSuspended(ctx, db, "operator", "target", 0, true)
	require.ErrorIs(t, err, ErrAdminRequired)
	_, err = db.Exec(`DELETE FROM account_email_verifications WHERE user_id='operator'`)
	require.NoError(t, err)
	require.NoError(t, SetPlatformAdmin(ctx, db, "operator", false))
	_, err = SetAccountSuspended(ctx, db, "operator", "target", 0, true)
	require.ErrorIs(t, err, ErrAdminRequired)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = SetAccountSuspended(cancelled, db, "operator", "target", 0, true)
	require.ErrorIs(t, err, context.Canceled)
}
