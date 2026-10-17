package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

var (
	// ErrAccountSuspended blocks access while retaining account data and API keys.
	ErrAccountSuspended = errors.New("account is suspended")
	// ErrAdminRequired denies mutations if the actor's durable authorization changed.
	ErrAdminRequired = errors.New("active platform administrator required")
	// ErrAdminProtected prevents self-suspension and accidental administrator lockout.
	ErrAdminProtected = errors.New("revoke administrator membership before suspending this account")
	// ErrAdminSessionRevoked rejects a JWT invalidated before the write transaction.
	ErrAdminSessionRevoked = errors.New("administrator session revoked")
)

// SetAccountSuspended rechecks authorization, revokes sessions and audits one transition atomically.
func SetAccountSuspended(ctx context.Context, db *sql.DB, actor, target string, sessionVersion int, suspended bool) (domain.AdminUser, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AdminUser{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// Acquire the SQLite writer lock before reading actor/target state; concurrent
	// CLI role revocations and account transitions cannot invalidate these checks.
	result, err := tx.ExecContext(ctx, `UPDATE platform_admins SET user_id=user_id WHERE user_id=?`, actor)
	if err != nil {
		return domain.AdminUser{}, err
	}
	matched, err := result.RowsAffected()
	if err != nil {
		return domain.AdminUser{}, err
	}
	if matched != 1 {
		return domain.AdminUser{}, ErrAdminRequired
	}
	access, err := readEmailVerificationStatus(ctx, tx, actor)
	if err != nil {
		return domain.AdminUser{}, err
	}
	if access.Suspended || (access.Required && !access.Verified) {
		return domain.AdminUser{}, ErrAdminRequired
	}
	if access.SessionVersion != sessionVersion {
		return domain.AdminUser{}, ErrAdminSessionRevoked
	}
	user, err := scanAdminUser(tx.QueryRowContext(ctx, adminUserSelect+`WHERE u.user_id=?`, target))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrUserNotFound
	}
	if err != nil {
		return domain.AdminUser{}, err
	}
	if suspended && user.Role == "admin" {
		return domain.AdminUser{}, ErrAdminProtected
	}
	if (user.Status == domain.AccountSuspended) == suspended {
		return user, tx.Commit()
	}
	if err := applyAccountStatus(ctx, tx, actor, target, suspended); err != nil {
		return domain.AdminUser{}, err
	}
	user.Status = domain.AccountActive
	if suspended {
		user.Status = domain.AccountSuspended
	}
	return user, tx.Commit()
}

func applyAccountStatus(ctx context.Context, tx *sql.Tx, actor, target string, suspended bool) error {
	query, action := `DELETE FROM account_suspensions WHERE user_id=?`, "activate"
	if suspended {
		query, action = `INSERT INTO account_suspensions(user_id) VALUES(?)`, "suspend"
	}
	if _, err := tx.ExecContext(ctx, query, target); err != nil {
		return err
	}
	// Preserve email proof and pending changes; never revive a pre-transition JWT.
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_email_verifications(user_id,session_version) VALUES(?,1)
 ON CONFLICT(user_id) DO UPDATE SET session_version=session_version+1`, target); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO platform_account_events(actor_user_id,target_user_id,action) VALUES(?,?,?)`, actor, target, action)
	return err
}
