package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Annany2002/nebula-backend/internal/avatar"
	"github.com/Annany2002/nebula-backend/internal/domain"
)

var (
	// ErrGitHubState covers unknown, expired, claimed and incorrectly bound OAuth attempts.
	ErrGitHubState = errors.New("invalid GitHub OAuth transaction")
	// ErrGitHubQuota bounds unexpired OAuth state, including consumed tombstones.
	ErrGitHubQuota = errors.New("GitHub OAuth transaction quota exceeded")
	// ErrGitHubConflict prevents implicit email linking and provider identity reassignment.
	ErrGitHubConflict = errors.New("GitHub identity conflicts with an existing account")
	// ErrGitHubAccount denies stale, unverified, missing or suspended accounts.
	ErrGitHubAccount = errors.New("GitHub account access denied")
)

func ensureGitHubOAuthTables(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS github_oauth_states (
 state_hash TEXT PRIMARY KEY,
 challenge TEXT NOT NULL,
 client_hash TEXT NOT NULL,
 link_user_id TEXT REFERENCES users(user_id) ON DELETE CASCADE,
 session_version INTEGER NOT NULL DEFAULT 0,
 claimed INTEGER NOT NULL DEFAULT 0 CHECK(claimed IN (0,1)),
 expires_at INTEGER NOT NULL
 );
 CREATE INDEX IF NOT EXISTS users_email_nocase ON users(email COLLATE NOCASE);
 CREATE INDEX IF NOT EXISTS github_oauth_expiry ON github_oauth_states(expires_at);
 CREATE INDEX IF NOT EXISTS github_oauth_client ON github_oauth_states(client_hash,expires_at);
 CREATE TABLE IF NOT EXISTS account_oauth_identities (
 provider TEXT NOT NULL CHECK(provider='github'),
 subject TEXT NOT NULL,
 user_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(provider,subject),
 UNIQUE(user_id,provider)
 );`)
	return err
}

// GitHubLinkIntent binds explicit account linking to password reauthentication and a live session.
type GitHubLinkIntent struct {
	UserID         string
	SessionVersion int
}

// ReserveGitHubState stores only the state digest and PKCE challenge, never secrets or codes.
func ReserveGitHubState(ctx context.Context, db *sql.DB, digest, challenge, peer string, link GitHubLinkIntent, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_oauth_states WHERE expires_at<=?`, now.Unix()); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM github_oauth_states`).Scan(&count); err != nil {
		return err
	}
	if count >= 10000 {
		return ErrGitHubQuota
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM github_oauth_states WHERE client_hash=? AND expires_at>?`, peer, now.Unix()).Scan(&count); err != nil {
		return err
	}
	if count >= 20 {
		return ErrGitHubQuota
	}
	var owner any
	if link.UserID != "" {
		status, readErr := readEmailVerificationStatus(ctx, tx, link.UserID)
		if readErr != nil {
			return readErr
		}
		if status.Suspended || (status.Required && !status.Verified) || status.SessionVersion != link.SessionVersion {
			return ErrGitHubAccount
		}
		owner = link.UserID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO github_oauth_states(state_hash,challenge,client_hash,link_user_id,session_version,expires_at) VALUES(?,?,?,?,?,?)`, digest, challenge, peer, owner, link.SessionVersion, now.Add(10*time.Minute).Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimGitHubState retires a correctly bound attempt before any non-retryable provider exchange.
func ClaimGitHubState(ctx context.Context, db *sql.DB, digest, challenge string, now time.Time) (GitHubLinkIntent, error) {
	var result GitHubLinkIntent
	err := db.QueryRowContext(ctx, `UPDATE github_oauth_states SET claimed=1 WHERE state_hash=? AND challenge=? AND expires_at>? AND claimed=0 RETURNING COALESCE(link_user_id,''),session_version`, digest, challenge, now.Unix()).Scan(&result.UserID, &result.SessionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrGitHubState
	}
	return result, err
}

// ResolveGitHubAccount atomically resolves immutable provider identity without email auto-linking.
// Existing username, email and verification state are never replaced by provider changes.
func ResolveGitHubAccount(ctx context.Context, db *sql.DB, subject, login, address string, required bool, link GitHubLinkIntent, avatarURL string) (*domain.UserMetadata, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Expired-state cleanup acquires the write lock before identity reads, serializing creation.
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_oauth_states WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		return nil, err
	}
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM account_oauth_identities WHERE provider='github' AND subject=?`, subject).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if link.UserID != "" {
		if owner != "" && owner != link.UserID {
			return nil, ErrGitHubConflict
		}
		if err := linkGitHubAccount(ctx, tx, subject, link); err != nil {
			return nil, err
		}
		owner = link.UserID
	} else if owner == "" {
		owner = uuid.NewString()
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=? COLLATE NOCASE)`, address).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrGitHubConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO users(user_id,username,email,password_hash) VALUES(?,?,?,'')`, owner, login, address); err != nil {
			return nil, userWriteError(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,verified_email,required) VALUES(?,?,?)`, owner, address, required); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_oauth_identities(provider,subject,user_id) VALUES('github',?,?)`, subject, owner); err != nil {
			return nil, err
		}
	}
	status, err := readEmailVerificationStatus(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if status.Suspended || (status.Required && !status.Verified) {
		return nil, ErrGitHubAccount
	}
	// Provider defaults may refresh, but never replace an explicit upload or removal.
	if imageURL := avatar.GitHubURL(avatarURL); imageURL != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_profiles(user_id,avatar_url,avatar_source,onboarding_completed) VALUES(?,?,'github',1)
   ON CONFLICT(user_id) DO UPDATE SET avatar_url=excluded.avatar_url,avatar_source='github',onboarding_completed=1 WHERE account_profiles.avatar_source IN ('','github')`, owner, imageURL); err != nil {
			return nil, err
		}
	}
	user, err := scanUser(tx.QueryRowContext(ctx, userSelect+`WHERE u.user_id=?`, owner))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func linkGitHubAccount(ctx context.Context, tx *sql.Tx, subject string, link GitHubLinkIntent) error {
	status, err := readEmailVerificationStatus(ctx, tx, link.UserID)
	if err != nil {
		return err
	}
	if status.Suspended || (status.Required && !status.Verified) || status.SessionVersion != link.SessionVersion {
		return ErrGitHubAccount
	}
	var current string
	err = tx.QueryRowContext(ctx, `SELECT subject FROM account_oauth_identities WHERE provider='github' AND user_id=?`, link.UserID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current != "" {
		return ErrGitHubConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_oauth_identities(provider,subject,user_id) VALUES('github',?,?)`, subject, link.UserID); err != nil {
		return err
	}
	// Linking adds a login credential. Revoke prior sessions and account-action links atomically.
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,session_version) VALUES(?,1) ON CONFLICT(user_id) DO UPDATE SET session_version=session_version+1,pending_email=''`, link.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_email_tokens WHERE user_id=?`, link.UserID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM account_password_reset_tokens WHERE user_id=?`, link.UserID)
	return err
}
