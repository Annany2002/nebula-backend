package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

var (
	// ErrVerificationToken hides whether a token was malformed, expired or already used.
	ErrVerificationToken = errors.New("invalid or expired verification token")
	// ErrVerificationRateLimit limits email delivery across requests and process restarts.
	ErrVerificationRateLimit = errors.New("verification request limit reached")
)

// VerificationRateLimitError reports the remaining persistent account cooldown.
type VerificationRateLimitError struct {
	RetryAfterSeconds int64
}

func (e *VerificationRateLimitError) Error() string { return ErrVerificationRateLimit.Error() }

// Unwrap preserves errors.Is checks without exposing account quotas on public routes.
func (e *VerificationRateLimitError) Unwrap() error { return ErrVerificationRateLimit }

func ensureEmailVerificationTables(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS account_email_verifications (
	 user_id TEXT PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
	 verified_email TEXT NOT NULL DEFAULT '',
	 required INTEGER NOT NULL DEFAULT 0 CHECK(required IN (0,1)),
	 pending_email TEXT NOT NULL DEFAULT '',
	 session_version INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS account_email_tokens (
	 token_hash TEXT PRIMARY KEY,
	 user_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
	 source_email TEXT NOT NULL,
	 target_email TEXT NOT NULL,
	 purpose TEXT NOT NULL CHECK(purpose IN ('verify_email','change_email')),
	 expires_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS account_email_tokens_owner ON account_email_tokens(user_id);
	CREATE TABLE IF NOT EXISTS email_send_limits (
	 user_id TEXT PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
	 last_requested_at INTEGER NOT NULL DEFAULT 0,
	 window_started_at INTEGER NOT NULL DEFAULT 0,
	 request_count INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS email_request_limits (
	 request_key TEXT PRIMARY KEY,
	 window_started_at INTEGER NOT NULL,
	 request_count INTEGER NOT NULL
	);`)
	return err
}

// CreateUnverifiedUser publishes the account and its access restriction atomically.
func CreateUnverifiedUser(ctx context.Context, db *sql.DB, id, username, email, passwordHash string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(user_id,username,email,password_hash) VALUES(?,?,?,?)`, id, username, email, passwordHash); err != nil {
		return userWriteError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,required) VALUES(?,1)`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// EmailVerificationStatus preserves access for legacy accounts without claiming verification.
type EmailVerificationStatus struct {
	Suspended      bool   `json:"-"`
	SessionVersion int    `json:"-"`
	Verified       bool   `json:"email_verified"`
	Required       bool   `json:"verification_required"`
	PendingEmail   string `json:"pending_email,omitempty"`
}

// GetEmailVerificationStatus checks the current address, so changing it cannot carry old proof.
func GetEmailVerificationStatus(ctx context.Context, db *sql.DB, id string) (EmailVerificationStatus, error) {
	return readEmailVerificationStatus(ctx, db, id)
}

type accountStatusReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readEmailVerificationStatus(ctx context.Context, db accountStatusReader, id string) (EmailVerificationStatus, error) {
	var result EmailVerificationStatus
	err := db.QueryRowContext(ctx, `SELECT COALESCE(v.verified_email=u.email,0),COALESCE(v.required,0),COALESCE(v.pending_email,''),COALESCE(v.session_version,0),EXISTS(SELECT 1 FROM account_suspensions s WHERE s.user_id=u.user_id) FROM users u LEFT JOIN account_email_verifications v ON v.user_id=u.user_id WHERE u.user_id=?`, id).Scan(&result.Verified, &result.Required, &result.PendingEmail, &result.SessionVersion, &result.Suspended)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrUserNotFound
	}
	return result, err
}

// AllowEmailRequest bounds public requests by a hashed client address, without trusting headers.
func AllowEmailRequest(ctx context.Context, db *sql.DB, key string, now time.Time) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now.Unix()
	cutoff := stamp - 15*60
	if _, err = tx.ExecContext(ctx, `DELETE FROM email_request_limits WHERE window_started_at<=?`, cutoff); err != nil {
		return false, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_request_limits`).Scan(&count); err != nil {
		return false, err
	}
	if count >= 10000 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM email_request_limits WHERE request_key=?)`, key).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO email_request_limits(request_key,window_started_at,request_count) VALUES(?,?,1) ON CONFLICT(request_key) DO UPDATE SET request_count=request_count+1 WHERE request_count<10`, key, stamp)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return affected == 1, nil
}

// ReserveEmailToken stores only a digest and reserves the persistent per-account send budget.
func ReserveEmailToken(ctx context.Context, db *sql.DB, id, source, target, purpose, digest string, now, expires time.Time) error {
	return reserveEmailToken(ctx, db, id, source, target, purpose, digest, now, expires, nil)
}

// ReserveEmailTokenForSession prevents an in-flight email change from surviving a password reset.
func ReserveEmailTokenForSession(ctx context.Context, db *sql.DB, id, source, target, purpose, digest string, now, expires time.Time, version int) error {
	return reserveEmailToken(ctx, db, id, source, target, purpose, digest, now, expires, &version)
}

func reserveEmailToken(ctx context.Context, db *sql.DB, id, source, target, purpose, digest string, now, expires time.Time, expectedVersion *int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Take the SQLite writer lock before reading quota and account state.
	if _, err = tx.ExecContext(ctx, `INSERT INTO email_send_limits(user_id) VALUES(?) ON CONFLICT(user_id) DO NOTHING`, id); err != nil {
		return err
	}
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE user_id=?`, id).Scan(&current); err != nil {
		return err
	}
	status, err := readEmailVerificationStatus(ctx, tx, id)
	if err != nil {
		return err
	}
	if current != source || (expectedVersion != nil && status.SessionVersion != *expectedVersion) || (purpose == "change_email" && status.Suspended) {
		return ErrVerificationToken
	}
	if err := reserveAccountMailBudget(ctx, tx, id, now); err != nil {
		return err
	}
	stamp := now.Unix()
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_email_tokens WHERE expires_at<=?`, stamp); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_email_tokens(token_hash,user_id,source_email,target_email,purpose,expires_at) VALUES(?,?,?,?,?,?)`, digest, id, source, target, purpose, expires.Unix()); err != nil {
		return err
	}
	if purpose == "change_email" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,pending_email) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET pending_email=excluded.pending_email`, id, target); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DiscardEmailToken removes a failed delivery without revoking previously delivered tokens.
func DiscardEmailToken(ctx context.Context, db *sql.DB, digest string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id, target, purpose string
	err = tx.QueryRowContext(ctx, `DELETE FROM account_email_tokens WHERE token_hash=? RETURNING user_id,target_email,purpose`, digest).Scan(&id, &target, &purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if purpose == "change_email" {
		_, err = tx.ExecContext(ctx, `UPDATE account_email_verifications SET pending_email=COALESCE((SELECT target_email FROM account_email_tokens WHERE user_id=? AND purpose='change_email' AND expires_at>? ORDER BY expires_at DESC,token_hash LIMIT 1),'') WHERE user_id=? AND pending_email=?`, id, time.Now().Unix(), id, target)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ConsumeEmailToken verifies or changes an address and retires all related intents atomically.
func ConsumeEmailToken(ctx context.Context, db *sql.DB, digest string, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id, source, target, purpose string
	// DELETE RETURNING serializes competing clicks without a separate read/write race.
	err = tx.QueryRowContext(ctx, `DELETE FROM account_email_tokens WHERE token_hash=? AND expires_at>? RETURNING user_id,source_email,target_email,purpose`, digest, now.Unix()).Scan(&id, &source, &target, &purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrVerificationToken
	}
	if err != nil {
		return err
	}
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE user_id=?`, id).Scan(&current); err != nil {
		return err
	}
	if current != source {
		return ErrVerificationToken
	}
	if purpose == "change_email" {
		var pending string
		if err := tx.QueryRowContext(ctx, `SELECT pending_email FROM account_email_verifications WHERE user_id=?`, id).Scan(&pending); err != nil {
			return err
		}
		if pending != target {
			return ErrVerificationToken
		}
		if _, err = tx.ExecContext(ctx, `UPDATE users SET email=? WHERE user_id=?`, target, id); err != nil {
			return userWriteError(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE account_email_verifications SET session_version=session_version+1 WHERE user_id=?`, id); err != nil {
			return err
		}
	} else if target != current {
		return ErrVerificationToken
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,verified_email,required) VALUES(?,?,1) ON CONFLICT(user_id) DO UPDATE SET verified_email=excluded.verified_email,required=1,pending_email=''`, id, target); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_email_tokens WHERE user_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func userWriteError(err error) error {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique && strings.Contains(sqliteErr.Error(), "users.email") {
		return ErrEmailExists
	}
	return fmt.Errorf("account write failed: %w", err)
}

// reserveAccountMailBudget runs under a transaction that already holds the SQLite writer lock.
func reserveAccountMailBudget(ctx context.Context, tx *sql.Tx, id string, now time.Time) error {
	stamp := now.Unix()
	result, err := tx.ExecContext(ctx, `UPDATE email_send_limits SET last_requested_at=?,window_started_at=CASE WHEN window_started_at<=? THEN ? ELSE window_started_at END,request_count=CASE WHEN window_started_at<=? THEN 1 ELSE request_count+1 END WHERE user_id=? AND last_requested_at<=? AND (window_started_at<=? OR request_count<5)`, stamp, stamp-3600, stamp, stamp-3600, id, stamp-60, stamp-3600)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		var last, window, requests int64
		if err := tx.QueryRowContext(ctx, `SELECT last_requested_at,window_started_at,request_count FROM email_send_limits WHERE user_id=?`, id).Scan(&last, &window, &requests); err != nil {
			return err
		}
		retry := max(int64(1), last+60-stamp)
		if requests >= 5 {
			retry = max(retry, window+3600-stamp)
		}
		return &VerificationRateLimitError{RetryAfterSeconds: retry}
	}
	return nil
}
