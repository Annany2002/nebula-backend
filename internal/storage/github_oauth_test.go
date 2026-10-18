package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

func githubStoreFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = CreateUser(context.Background(), db, "owner", "owner_name", "owner@example.test", "hash")
	require.NoError(t, err)
	return db
}

func TestGitHubStatesAreBoundedPersistentAndConsumedOnce(t *testing.T) {
	db := githubStoreFixture(t)
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, ReserveGitHubState(ctx, db, "digest", "challenge", "peer", GitHubLinkIntent{}, now))
	_, err := ClaimGitHubState(ctx, db, "digest", "wrong", now)
	require.ErrorIs(t, err, ErrGitHubState)
	results := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := ClaimGitHubState(ctx, db, "digest", "challenge", now)
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrGitHubState)
		}
	}
	require.Equal(t, 1, success)
	// Consumed tombstones still count toward the budget, which survives another connection.
	for i := 1; i < 20; i++ {
		require.NoError(t, ReserveGitHubState(ctx, db, fmt.Sprint(i), "challenge", "peer", GitHubLinkIntent{}, now))
	}
	require.ErrorIs(t, ReserveGitHubState(ctx, db, "over", "challenge", "peer", GitHubLinkIntent{}, now), ErrGitHubQuota)
	require.NoError(t, ReserveGitHubState(ctx, db, "other", "challenge", "different-peer", GitHubLinkIntent{}, now))
	_, err = ClaimGitHubState(ctx, db, "other", "challenge", now.Add(10*time.Minute))
	require.ErrorIs(t, err, ErrGitHubState)
	require.NoError(t, ReserveGitHubState(ctx, db, "fresh", "challenge", "peer", GitHubLinkIntent{}, now.Add(10*time.Minute)))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM github_oauth_states`).Scan(&count))
	require.Equal(t, 1, count)
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT COUNT(*) FROM github_oauth_states WHERE client_hash=? AND expires_at>?`, "peer", now.Unix())
	require.NoError(t, err)
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plans = append(plans, detail)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plans, " "), "github_oauth_client")
}

func TestGitHubNewAccountsAreAtomicAndProviderIdentityIsStable(t *testing.T) {
	db := githubStoreFixture(t)
	ctx := context.Background()
	user, err := ResolveGitHubAccount(ctx, db, "12345", "octocat", "verified@example.test", true, GitHubLinkIntent{})
	require.NoError(t, err)
	require.True(t, user.EmailVerified)
	require.True(t, user.VerificationRequired)
	require.Empty(t, user.PasswordHash)
	require.Equal(t, "user", user.Role)
	returned, err := ResolveGitHubAccount(ctx, db, "12345", "changed-login", "different@example.test", false, GitHubLinkIntent{})
	require.NoError(t, err)
	require.Equal(t, user.UserId, returned.UserId)
	require.Equal(t, user.Email, returned.Email)
	require.Equal(t, user.Username, returned.Username)
	_, err = ResolveGitHubAccount(ctx, db, "9999", "other", "OWNER@example.test", false, GitHubLinkIntent{})
	require.ErrorIs(t, err, ErrGitHubConflict)
	_, err = db.Exec(`INSERT INTO account_suspensions(user_id) VALUES(?)`, user.UserId)
	require.NoError(t, err)
	_, err = ResolveGitHubAccount(ctx, db, "12345", "octocat", "verified@example.test", true, GitHubLinkIntent{})
	require.ErrorIs(t, err, ErrGitHubAccount)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count))
	require.Equal(t, 2, count)
	// Failure after inserting an account rolls back both identity and email proof.
	_, err = db.Exec(`CREATE TRIGGER reject_github_identity BEFORE INSERT ON account_oauth_identities BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	require.NoError(t, err)
	_, err = ResolveGitHubAccount(ctx, db, "7777", "rollback", "rollback@example.test", true, GitHubLinkIntent{})
	require.Error(t, err)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users WHERE email='rollback@example.test'`).Scan(&count))
	require.Zero(t, count)
}

func TestGitHubExplicitLinkIsSessionBoundAndRevokesExistingCredentials(t *testing.T) {
	db := githubStoreFixture(t)
	ctx := context.Background()
	intent := GitHubLinkIntent{UserID: "owner", SessionVersion: 0}
	_, err := db.Exec(`INSERT INTO account_email_verifications(user_id,pending_email) VALUES('owner','pending@example.test'); INSERT INTO account_email_tokens(token_hash,user_id,source_email,target_email,purpose,expires_at) VALUES('email','owner','owner@example.test','pending@example.test','change_email',9999999999); INSERT INTO account_password_reset_tokens(token_hash,user_id,source_email,session_version,expires_at) VALUES('reset','owner','owner@example.test',0,9999999999);`)
	require.NoError(t, err)
	user, err := ResolveGitHubAccount(ctx, db, "12345", "octocat", "different@example.test", true, intent)
	require.NoError(t, err)
	require.Equal(t, "owner", user.UserId)
	require.Equal(t, "owner@example.test", user.Email)
	require.Equal(t, 1, user.SessionVersion)
	require.Empty(t, user.PendingEmail)
	require.Equal(t, "hash", user.PasswordHash)
	for _, table := range []string{"account_email_tokens", "account_password_reset_tokens"} {
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count)
	}
	_, err = ResolveGitHubAccount(ctx, db, "other", "other", "other@example.test", false, intent)
	require.ErrorIs(t, err, ErrGitHubAccount)
	_, err = ResolveGitHubAccount(ctx, db, "12345", "octocat", "changed@example.test", false, GitHubLinkIntent{UserID: "owner", SessionVersion: 1})
	require.ErrorIs(t, err, ErrGitHubConflict)
	// A different local account cannot claim an existing immutable GitHub subject.
	_, err = CreateUser(ctx, db, "other", "other_account", "other@example.test", "hash")
	require.NoError(t, err)
	_, err = ResolveGitHubAccount(ctx, db, "12345", "octocat", "verified@example.test", false, GitHubLinkIntent{UserID: "other"})
	require.ErrorIs(t, err, ErrGitHubConflict)
}

func TestGitHubConcurrentCreationKeepsOneAccountAndIdentity(t *testing.T) {
	db := githubStoreFixture(t)
	ctx := context.Background()
	var workers sync.WaitGroup
	ids := make(chan string, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			user, err := ResolveGitHubAccount(ctx, db, "12345", "octocat", "verified@example.test", false, GitHubLinkIntent{})
			if err != nil {
				failures <- err
				return
			}
			ids <- user.UserId
		}()
	}
	workers.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		require.Equal(t, first, id)
	}
	require.NotEmpty(t, first)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users WHERE email='verified@example.test'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM account_oauth_identities`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestGitHubLinkRejectsChangedAndDeletedAccounts(t *testing.T) {
	for _, change := range []string{
		`INSERT INTO account_suspensions(user_id) VALUES('owner')`,
		`INSERT INTO account_email_verifications(user_id,required) VALUES('owner',1)`,
		`INSERT INTO account_email_verifications(user_id,session_version) VALUES('owner',1)`,
		`DELETE FROM users WHERE user_id='owner'`,
	} {
		t.Run(change, func(t *testing.T) {
			db := githubStoreFixture(t)
			ctx := context.Background()
			require.NoError(t, ReserveGitHubState(ctx, db, "digest", "challenge", "peer", GitHubLinkIntent{UserID: "owner"}, time.Now()))
			_, err := db.Exec(change)
			require.NoError(t, err)
			_, err = ResolveGitHubAccount(ctx, db, "12345", "octocat", "verified@example.test", false, GitHubLinkIntent{UserID: "owner"})
			require.Error(t, err)
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM account_oauth_identities`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestGitHubStateQuotaSurvivesRestartAndBoundsGlobalCapacity(t *testing.T) {
	cfg := &config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"}
	db, err := ConnectMetadataDB(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 20; i++ {
		require.NoError(t, ReserveGitHubState(ctx, db, fmt.Sprint(i), "challenge", "peer", GitHubLinkIntent{}, now))
	}
	require.NoError(t, db.Close())
	db, err = ConnectMetadataDB(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.ErrorIs(t, ReserveGitHubState(ctx, db, "new", "challenge", "peer", GitHubLinkIntent{}, now), ErrGitHubQuota)
	_, err = db.Exec(`WITH RECURSIVE numbers(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM numbers WHERE x<9980) INSERT INTO github_oauth_states(state_hash,challenge,client_hash,expires_at) SELECT 'global-'||x,'challenge','global-'||x,? FROM numbers`, now.Add(10*time.Minute).Unix())
	require.NoError(t, err)
	require.ErrorIs(t, ReserveGitHubState(ctx, db, "beyond-global", "challenge", "new-peer", GitHubLinkIntent{}, now), ErrGitHubQuota)
}

func TestGitHubIdentityAndExpiryLookupsUseIndexes(t *testing.T) {
	db := githubStoreFixture(t)
	for _, tc := range []struct {
		query, index string
		args         []any
	}{
		{`SELECT user_id FROM account_oauth_identities WHERE provider='github' AND subject=?`, "sqlite_autoindex_account_oauth_identities", []any{"12345"}},
		{`SELECT subject FROM account_oauth_identities WHERE provider='github' AND user_id=?`, "sqlite_autoindex_account_oauth_identities", []any{"owner"}},
		{`SELECT EXISTS(SELECT 1 FROM users WHERE email=? COLLATE NOCASE)`, "users_email_nocase", []any{"OWNER@example.test"}},
		{`DELETE FROM github_oauth_states WHERE expires_at<=?`, "github_oauth_expiry", []any{time.Now().Unix()}},
		{`SELECT state_hash FROM github_oauth_states WHERE state_hash=?`, "sqlite_autoindex_github_oauth_states", []any{"digest"}},
	} {
		t.Run(tc.index+tc.query, func(t *testing.T) {
			rows, err := db.Query(`EXPLAIN QUERY PLAN `+tc.query, tc.args...)
			require.NoError(t, err)
			defer rows.Close()
			var plans []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
				plans = append(plans, detail)
			}
			require.NoError(t, rows.Err())
			require.Contains(t, strings.Join(plans, " "), tc.index)
		})
	}
}
