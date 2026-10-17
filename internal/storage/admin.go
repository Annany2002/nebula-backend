package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

// ErrInvalidAdminPage rejects unbounded or negative metadata scans.
var ErrInvalidAdminPage = errors.New("admin limit must be 1-100 and offset must be 0-1000000")

func ensureAdminSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS platform_admins (
	 user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
	 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_users_admin_created ON users(created_at DESC,user_id ASC);
	CREATE TABLE IF NOT EXISTS account_suspensions (
	 user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
	 suspended_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS platform_account_events (
	 event_id INTEGER PRIMARY KEY AUTOINCREMENT,
	 actor_user_id TEXT NOT NULL,
	 target_user_id TEXT NOT NULL,
	 action TEXT NOT NULL CHECK(action IN ('suspend','activate')),
	 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`)
	return err
}

// IsPlatformAdmin reads current membership rather than trusting a role in a JWT.
func IsPlatformAdmin(ctx context.Context, db *sql.DB, userID string) (bool, error) {
	var allowed bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_admins WHERE user_id=?)`, userID).Scan(&allowed)
	return allowed, err
}

// SetPlatformAdmin is for trusted operators; no HTTP route exposes role changes.
// Repeating a grant or revoke is safe, but nonexistent accounts are rejected.
func SetPlatformAdmin(ctx context.Context, db *sql.DB, userID string, enabled bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// A single statement acquires the writer lock before the existence check.
	query := `DELETE FROM platform_admins WHERE user_id=?`
	if enabled {
		query = `INSERT INTO platform_admins(user_id) SELECT user_id FROM users WHERE user_id=? ON CONFLICT(user_id) DO NOTHING`
	}
	if _, err = tx.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("change platform admin membership: %w", err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE user_id=?)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrUserNotFound
	}
	if enabled {
		var suspended bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_suspensions WHERE user_id=?)`, userID).Scan(&suspended); err != nil {
			return err
		}
		if suspended {
			return ErrAccountSuspended
		}
	}
	return tx.Commit()
}

const adminUserSelect = `SELECT u.user_id,u.username,u.email,u.created_at,
 COALESCE(v.verified_email=u.email,0),COALESCE(v.required,0),
 CASE WHEN EXISTS(SELECT 1 FROM platform_admins a WHERE a.user_id=u.user_id) THEN 'admin' ELSE 'user' END,
 (SELECT COUNT(*) FROM databases d WHERE d.owner_id=u.user_id),
 CASE WHEN EXISTS(SELECT 1 FROM account_suspensions s WHERE s.user_id=u.user_id) THEN 'suspended' ELSE 'active' END
 FROM users u LEFT JOIN account_email_verifications v ON v.user_id=u.user_id `

type adminUserScanner interface{ Scan(...any) error }

func scanAdminUser(row adminUserScanner) (domain.AdminUser, error) {
	var user domain.AdminUser
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.CreatedAt,
		&user.EmailVerified, &user.VerificationRequired, &user.Role, &user.DatabaseCount, &user.Status)
	return user, err
}

// ListAdminUsers reads the total and deterministically ordered page from one snapshot.
func ListAdminUsers(ctx context.Context, db *sql.DB, limit, offset int) ([]domain.AdminUser, int64, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrInvalidAdminPage
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, adminUserSelect+`ORDER BY u.created_at DESC,u.user_id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]domain.AdminUser, 0, limit)
	for rows.Next() {
		user, scanErr := scanAdminUser(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// GetAdminUser returns the same safe projection as the list endpoint.
func GetAdminUser(ctx context.Context, db *sql.DB, userID string) (domain.AdminUser, error) {
	user, err := scanAdminUser(db.QueryRowContext(ctx, adminUserSelect+`WHERE u.user_id=?`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrUserNotFound
	}
	return user, err
}
