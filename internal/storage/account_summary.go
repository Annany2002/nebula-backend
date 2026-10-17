package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

// GetAccountSummary uses one metadata snapshot and never accepts an account scope from a query string.
func GetAccountSummary(ctx context.Context, db *sql.DB, userID string) (domain.AccountSummary, error) {
	var summary domain.AccountSummary
	err := db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM databases d WHERE d.owner_id=u.user_id),
 (SELECT COUNT(*) FROM api_keys k JOIN databases d ON d.database_id=k.api_database_id AND d.owner_id=k.api_owner_id WHERE k.api_owner_id=u.user_id),
 (SELECT COUNT(*) FROM database_backups b WHERE b.owner_id=u.user_id AND b.status='ready')
 FROM users u WHERE u.user_id=?`, userID).Scan(&summary.DatabaseCount, &summary.ActiveAPIKeyCount, &summary.ReadyBackupCount)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrUserNotFound
	}
	return summary, err
}
