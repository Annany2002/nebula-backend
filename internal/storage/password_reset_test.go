package storage

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

func resetStorageFixture(t *testing.T) (*sql.DB, time.Time) {
	t.Helper()
	db, err := ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = CreateUser(context.Background(), db, "owner", "owner", "owner@example.test", "old-hash")
	require.NoError(t, err)
	return db, time.Now().UTC()
}

func TestPasswordResetIsAtomicSingleUseAndRevokesAllIntents(t *testing.T) {
	db, now := resetStorageFixture(t)
	ctx := context.Background()
	require.NoError(t, ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "first", now, now.Add(time.Hour)))
	require.NoError(t, ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "second", now.Add(time.Minute), now.Add(time.Hour)))
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", "owner@example.test", "pending@example.test", "change_email", "email-token", now.Add(2*time.Minute), now.Add(time.Hour)))
	results := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := ConsumePasswordReset(ctx, db, "first", "new-hash", now)
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrPasswordResetToken)
		}
	}
	require.Equal(t, 1, successes)
	user, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.Equal(t, "new-hash", user.PasswordHash)
	require.Equal(t, 1, user.SessionVersion)
	require.False(t, user.EmailVerified)
	require.False(t, user.VerificationRequired)
	require.Empty(t, user.PendingEmail)
	_, err = ConsumePasswordReset(ctx, db, "second", "attacker-hash", now)
	require.ErrorIs(t, err, ErrPasswordResetToken)
	require.ErrorIs(t, ConsumeEmailToken(ctx, db, "email-token", now), ErrVerificationToken)
}

func TestPasswordResetRejectsChangedIdentitySuspensionAndExpiry(t *testing.T) {
	for _, mode := range []string{"expired", "email changed", "session revoked", "suspended", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			db, now := resetStorageFixture(t)
			ctx := context.Background()
			require.NoError(t, ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "token", now, now.Add(time.Hour)))
			var err error
			switch mode {
			case "expired":
				_, err = db.Exec(`UPDATE account_password_reset_tokens SET expires_at=?`, now.Unix())
			case "email changed":
				_, err = db.Exec(`UPDATE users SET email='changed@example.test'`)
			case "session revoked":
				_, err = db.Exec(`INSERT INTO account_email_verifications(user_id,session_version) VALUES('owner',1)`)
			case "suspended":
				_, err = db.Exec(`INSERT INTO account_suspensions(user_id) VALUES('owner')`)
			case "deleted":
				_, err = db.Exec(`DELETE FROM users WHERE user_id='owner'`)
			}
			require.NoError(t, err)
			eligible, err := PasswordResetEligible(ctx, db, "token", now)
			require.NoError(t, err)
			require.False(t, eligible)
			_, err = ConsumePasswordReset(ctx, db, "token", "new-hash", now)
			require.ErrorIs(t, err, ErrPasswordResetToken)
			if mode != "deleted" {
				var hash string
				require.NoError(t, db.QueryRow(`SELECT password_hash FROM users WHERE user_id='owner'`).Scan(&hash))
				require.Equal(t, "old-hash", hash)
			}
		})
	}
}

func TestPasswordResetDeliveryBudgetFailureAndRollback(t *testing.T) {
	db, now := resetStorageFixture(t)
	ctx := context.Background()
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", "owner@example.test", "owner@example.test", "verify_email", "verification", now, now.Add(time.Hour)))
	err := ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "throttled", now, now.Add(time.Hour))
	require.ErrorIs(t, err, ErrVerificationRateLimit)
	require.NoError(t, ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "older", now.Add(time.Minute), now.Add(time.Hour)))
	require.NoError(t, ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "failed", now.Add(2*time.Minute), now.Add(time.Hour)))
	require.NoError(t, DiscardPasswordResetToken(ctx, db, "failed"))
	eligible, err := PasswordResetEligible(ctx, db, "older", now)
	require.NoError(t, err)
	require.True(t, eligible)
	eligible, err = PasswordResetEligible(ctx, db, "failed", now)
	require.NoError(t, err)
	require.False(t, eligible)
	// A late storage error must roll back password, token deletion and session revocation together.
	_, err = db.Exec(`CREATE TRIGGER fail_reset BEFORE UPDATE OF password_hash ON users BEGIN SELECT RAISE(ABORT,'fixture'); END;`)
	require.NoError(t, err)
	_, err = ConsumePasswordReset(ctx, db, "older", "new-hash", now)
	require.Error(t, err)
	eligible, err = PasswordResetEligible(ctx, db, "older", now)
	require.NoError(t, err)
	require.True(t, eligible)
	user, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.Equal(t, "old-hash", user.PasswordHash)
	require.Zero(t, user.SessionVersion)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = ConsumePasswordReset(cancelled, db, "older", "new-hash", now)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPasswordResetBlocksAlreadyAuthorizedEmailChanges(t *testing.T) {
	db, now := resetStorageFixture(t)
	ctx := context.Background()
	require.NoError(t, ReservePasswordResetToken(ctx, db, "owner", "owner@example.test", "token", now, now.Add(time.Hour)))
	_, err := ConsumePasswordReset(ctx, db, "token", "new-hash", now)
	require.NoError(t, err)
	err = ReserveEmailTokenForSession(ctx, db, "owner", "owner@example.test", "attacker@example.test", "change_email", "late-email-token", now.Add(time.Minute), now.Add(time.Hour), 0)
	require.ErrorIs(t, err, ErrVerificationToken)
	status, err := GetEmailVerificationStatus(ctx, db, "owner")
	require.NoError(t, err)
	require.Empty(t, status.PendingEmail)
}
