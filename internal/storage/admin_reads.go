package storage

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

// AdminUserFilter combines literal account search with current role and access state.
type AdminUserFilter struct{ Query, Role, Status string }

// AdminAuditFilter matches persisted IDs, so deleted accounts remain filterable.
type AdminAuditFilter struct{ ActorUserID, TargetUserID, Action string }

func validAdminText(value string, maxRunes int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func adminUserPredicate(filter AdminUserFilter) (string, []any, error) {
	if !validAdminText(filter.Query, 100) || (filter.Role != "" && filter.Role != "user" && filter.Role != "admin") || (filter.Status != "" && filter.Status != "active" && filter.Status != "suspended") {
		return "", nil, ErrInvalidAdminQuery
	}
	where, args := "WHERE 1=1", make([]any, 0, 4)
	if query := strings.TrimSpace(filter.Query); query != "" {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
		where += ` AND (u.username LIKE ? ESCAPE '\' OR u.email LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}
	if filter.Role != "" {
		where += ` AND EXISTS(SELECT 1 FROM platform_admins a WHERE a.user_id=u.user_id)=?`
		args = append(args, filter.Role == "admin")
	}
	if filter.Status != "" {
		where += ` AND EXISTS(SELECT 1 FROM account_suspensions s WHERE s.user_id=u.user_id)=?`
		args = append(args, filter.Status == "suspended")
	}
	return where, args, nil
}

type adminPageQuery struct {
	countSQL, selectSQL string
	args                []any
}

func readAdminPage[T any](ctx context.Context, db *sql.DB, query adminPageQuery, limit, offset int, scan func(adminUserScanner) (T, error)) ([]T, int64, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 1_000_000 {
		return nil, 0, ErrInvalidAdminQuery
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	if err := tx.QueryRowContext(ctx, query.countSQL, query.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append(append([]any{}, query.args...), limit, offset)
	rows, err := tx.QueryContext(ctx, query.selectSQL+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]T, 0, limit)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
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
	return items, total, nil
}

// ListAdminAuditEvents returns actual status transitions without joining mutable user data.
func ListAdminAuditEvents(ctx context.Context, db *sql.DB, limit, offset int, filter AdminAuditFilter) ([]domain.AdminAuditEvent, int64, error) {
	if !validAdminText(filter.ActorUserID, 128) || !validAdminText(filter.TargetUserID, 128) || (filter.Action != "" && filter.Action != "suspend" && filter.Action != "activate") {
		return nil, 0, ErrInvalidAdminQuery
	}
	where, args := "WHERE 1=1", make([]any, 0, 3)
	if filter.ActorUserID != "" {
		where += " AND actor_user_id=?"
		args = append(args, filter.ActorUserID)
	}
	if filter.TargetUserID != "" {
		where += " AND target_user_id=?"
		args = append(args, filter.TargetUserID)
	}
	if filter.Action != "" {
		where += " AND action=?"
		args = append(args, filter.Action)
	}
	query := adminPageQuery{
		countSQL:  `SELECT COUNT(*) FROM platform_account_events ` + where,
		selectSQL: `SELECT event_id,actor_user_id,target_user_id,action,created_at FROM platform_account_events ` + where + ` ORDER BY event_id DESC`,
		args:      args,
	}
	return readAdminPage(ctx, db, query, limit, offset, scanAdminAuditEvent)
}

func scanAdminAuditEvent(row adminUserScanner) (domain.AdminAuditEvent, error) {
	var event domain.AdminAuditEvent
	err := row.Scan(&event.EventID, &event.ActorUserID, &event.TargetUserID, &event.Action, &event.CreatedAt)
	return event, err
}
