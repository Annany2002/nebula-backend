package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrPasswordResetToken hides malformed, expired, superseded and already consumed reset links.
var ErrPasswordResetToken = errors.New("invalid or expired password reset token")

func ensurePasswordResetTables(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS account_password_reset_tokens (
 token_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
 source_email TEXT NOT NULL,
 session_version INTEGER NOT NULL,
 expires_at INTEGER NOT NULL
 );
 CREATE INDEX IF NOT EXISTS account_password_reset_owner ON account_password_reset_tokens(user_id);
 CREATE INDEX IF NOT EXISTS account_password_reset_expiry ON account_password_reset_tokens(expires_at);`)
	return err
}

// ReservePasswordResetToken shares the mail budget without revoking previously delivered links.
func ReservePasswordResetToken(ctx context.Context, db *sql.DB, id, address, digest string, now, expires time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO email_send_limits(user_id) VALUES(?) ON CONFLICT(user_id) DO NOTHING`, id); err != nil {
		return err
	}
	var current string
	var version int
	var suspended bool
	err = tx.QueryRowContext(ctx, `SELECT u.email,COALESCE(v.session_version,0),EXISTS(SELECT 1 FROM account_suspensions s WHERE s.user_id=u.user_id) FROM users u LEFT JOIN account_email_verifications v ON v.user_id=u.user_id WHERE u.user_id=?`, id).Scan(&current, &version, &suspended)
	if err != nil {
		return err
	}
	if current != address || suspended {
		return ErrPasswordResetToken
	}
	if err := reserveAccountMailBudget(ctx, tx, id, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_password_reset_tokens WHERE expires_at<=?`, now.Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_password_reset_tokens(token_hash,user_id,source_email,session_version,expires_at) VALUES(?,?,?,?,?)`, digest, id, address, version, expires.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// DiscardPasswordResetToken retires only an unconfirmed delivery.
func DiscardPasswordResetToken(ctx context.Context, db *sql.DB, digest string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM account_password_reset_tokens WHERE token_hash=?`, digest)
	return err
}

// PasswordResetEligible avoids bcrypt work on invalid tokens. Consumption still rechecks atomically.
func PasswordResetEligible(ctx context.Context, db *sql.DB, digest string, now time.Time) (bool, error) {
	var eligible bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_password_reset_tokens t JOIN users u ON u.user_id=t.user_id LEFT JOIN account_email_verifications v ON v.user_id=u.user_id WHERE t.token_hash=? AND t.expires_at>? AND t.source_email=u.email AND t.session_version=COALESCE(v.session_version,0) AND NOT EXISTS(SELECT 1 FROM account_suspensions s WHERE s.user_id=u.user_id))`, digest, now.Unix()).Scan(&eligible)
	return eligible, err
}

// ConsumePasswordReset changes credentials, revokes sessions and retires pending links in one commit.
// Verification state, API keys, tenant data and suspension are never changed by password recovery.
func ConsumePasswordReset(ctx context.Context, db *sql.DB, digest, passwordHash string, now time.Time) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var id, source string
	var version int
	err = tx.QueryRowContext(ctx, `DELETE FROM account_password_reset_tokens WHERE token_hash=? AND expires_at>? RETURNING user_id,source_email,session_version`, digest, now.Unix()).Scan(&id, &source, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrPasswordResetToken
	}
	if err != nil {
		return "", err
	}
	status, err := readEmailVerificationStatus(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if status.Suspended || status.SessionVersion != version {
		return "", ErrPasswordResetToken
	}
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE user_id=? AND email=?`, passwordHash, id, source)
	if err != nil {
		return "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if changed != 1 {
		return "", ErrPasswordResetToken
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,session_version) VALUES(?,1) ON CONFLICT(user_id) DO UPDATE SET session_version=session_version+1,pending_email=''`, id); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_email_tokens WHERE user_id=?`, id); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_password_reset_tokens WHERE user_id=?`, id); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return source, nil
}
