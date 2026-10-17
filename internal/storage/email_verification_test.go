package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

func TestEmailVerificationAtomicConsumptionAndLegacyAccess(t *testing.T) {
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	db, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	now := time.Now()
	_, err = CreateUser(ctx, db, "legacy", "legacy", "legacy@example.test", "hash")
	require.NoError(t, err)
	status, err := GetEmailVerificationStatus(ctx, db, "legacy")
	require.NoError(t, err)
	require.False(t, status.Required)
	require.False(t, status.Verified)
	require.NoError(t, CreateUnverifiedUser(ctx, db, "pending", "pending", "pending@example.test", "hash"))
	status, err = GetEmailVerificationStatus(ctx, db, "pending")
	require.NoError(t, err)
	require.True(t, status.Required)
	require.False(t, status.Verified)
	require.NoError(t, ReserveEmailToken(ctx, db, "pending", "pending@example.test", "pending@example.test", "verify_email", "digest", now, now.Add(time.Hour)))
	errors := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); errors <- ConsumeEmailToken(ctx, db, "digest", now) }()
	}
	workers.Wait()
	close(errors)
	count := 0
	for err := range errors {
		if err == nil {
			count++
		} else {
			require.ErrorIs(t, err, ErrVerificationToken)
		}
	}
	require.Equal(t, 1, count)
	status, err = GetEmailVerificationStatus(ctx, db, "pending")
	require.NoError(t, err)
	require.True(t, status.Verified)
	require.NoError(t, db.Close())
	reopened, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer reopened.Close()
	status, err = GetEmailVerificationStatus(ctx, reopened, "legacy")
	require.NoError(t, err)
	require.False(t, status.Required)
	require.ErrorIs(t, ConsumeEmailToken(ctx, reopened, "digest", now), ErrVerificationToken)
}

func TestEmailTokenExpiryConflictsAndFailedDelivery(t *testing.T) {
	db, err := ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	now := time.Now()
	_, err = CreateUser(ctx, db, "owner", "owner", "old@example.test", "hash")
	require.NoError(t, err)
	_, err = CreateUser(ctx, db, "other", "other", "taken@example.test", "hash")
	require.NoError(t, err)
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", "old@example.test", "old@example.test", "verify_email", "expired", now.Add(-time.Hour), now.Add(-time.Second)))
	require.ErrorIs(t, ConsumeEmailToken(ctx, db, "expired", now), ErrVerificationToken)
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", "old@example.test", "first@example.test", "change_email", "first", now, now.Add(time.Hour)))
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", "old@example.test", "failed@example.test", "change_email", "failed", now.Add(time.Minute), now.Add(time.Hour+time.Minute)))
	require.NoError(t, DiscardEmailToken(ctx, db, "failed"))
	status, err := GetEmailVerificationStatus(ctx, db, "owner")
	require.NoError(t, err)
	require.Equal(t, "first@example.test", status.PendingEmail)
	require.NoError(t, ConsumeEmailToken(ctx, db, "first", now))
	user, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.Equal(t, "first@example.test", user.Email)
	require.Equal(t, 1, user.SessionVersion)
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", user.Email, "taken@example.test", "change_email", "conflict", now.Add(2*time.Minute), now.Add(time.Hour)))
	require.ErrorIs(t, ConsumeEmailToken(ctx, db, "conflict", now), ErrEmailExists)
	user, err = FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.Equal(t, "first@example.test", user.Email)
	require.Equal(t, 1, user.SessionVersion)
	_, err = db.Exec(`UPDATE users SET email='different@example.test' WHERE user_id='owner'`)
	require.NoError(t, err)
	require.ErrorIs(t, ConsumeEmailToken(ctx, db, "conflict", now), ErrVerificationToken)
}

func TestEmailDeliveryQuotasAndRequestLimitsArePersistent(t *testing.T) {
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	db, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	now := time.Now()
	_, err = CreateUser(ctx, db, "owner", "owner", "owner@example.test", "hash")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, ReserveEmailToken(ctx, db, "owner", "owner@example.test", "owner@example.test", "verify_email", fmt.Sprint(i), now.Add(time.Duration(i)*time.Minute), now.Add(time.Hour)))
	}
	limitErr := ReserveEmailToken(ctx, db, "owner", "owner@example.test", "owner@example.test", "verify_email", "excess", now.Add(6*time.Minute), now.Add(time.Hour))
	require.ErrorIs(t, limitErr, ErrVerificationRateLimit)
	var limit *VerificationRateLimitError
	require.ErrorAs(t, limitErr, &limit)
	require.Equal(t, int64(54*60), limit.RetryAfterSeconds)
	key := sha256.Sum256([]byte("client"))
	hashed := hex.EncodeToString(key[:])
	for i := 0; i < 10; i++ {
		allowed, err := AllowEmailRequest(ctx, db, hashed, now)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	require.NoError(t, db.Close())
	db, err = ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer db.Close()
	allowed, err := AllowEmailRequest(ctx, db, hashed, now)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = AllowEmailRequest(ctx, db, hashed, now.Add(15*time.Minute))
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, ReserveEmailToken(ctx, db, "owner", "owner@example.test", "owner@example.test", "verify_email", "next-window", now.Add(time.Hour), now.Add(2*time.Hour)))
	limitErr = ReserveEmailToken(ctx, db, "owner", "owner@example.test", "owner@example.test", "verify_email", "cooldown", now.Add(time.Hour+10*time.Second), now.Add(2*time.Hour))
	require.ErrorAs(t, limitErr, &limit)
	require.Equal(t, int64(50), limit.RetryAfterSeconds)
}

func TestEmailRequestCapacityPreservesExistingBudgets(t *testing.T) {
	db, err := ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	defer db.Close()
	now := time.Now()
	_, err = db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO email_request_limits SELECT CAST(x AS TEXT),?,1 FROM n`, now.Unix())
	require.NoError(t, err)
	allowed, err := AllowEmailRequest(context.Background(), db, "1", now)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = AllowEmailRequest(context.Background(), db, "new-client", now)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = AllowEmailRequest(context.Background(), db, "new-client", now.Add(15*time.Minute))
	require.NoError(t, err)
	require.True(t, allowed)
}
