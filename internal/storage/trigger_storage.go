package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"

	"github.com/Annany2002/nebula-backend/internal/core"
	"github.com/Annany2002/nebula-backend/internal/domain"
)

var (
	ErrInvalidTrigger  = errors.New("invalid trigger definition")
	ErrTriggerExists   = errors.New("trigger name is already in use")
	ErrTriggerNotFound = errors.New("trigger not found")
)

// CreateTrigger validates and compiles a table trigger before committing its definition.
// Preparing one statement, then checking its stored SQL, prevents body/condition text
// from appending another top-level statement. Trigger actions are never executed here.
func CreateTrigger(ctx context.Context, db *sql.DB, definition *domain.TriggerDefinition) (domain.TriggerInfo, error) {
	empty := domain.TriggerInfo{}
	if definition == nil {
		return empty, fmt.Errorf("%w: provide a trigger definition", ErrInvalidTrigger)
	}
	if !core.IsValidIdentifier(definition.Name) || protectedSchemaName(definition.Name) {
		return empty, fmt.Errorf("%w: name must contain 1–64 letters, digits or underscores without an internal prefix", ErrInvalidTrigger)
	}
	if definition.TableName == "" || strings.ContainsRune(definition.TableName, 0) || protectedSchemaName(definition.TableName) {
		return empty, fmt.Errorf("%w: choose a user table without an internal prefix", ErrInvalidTrigger)
	}
	timing := strings.ToUpper(strings.TrimSpace(definition.Timing))
	if timing == "" {
		timing = "AFTER"
	}
	if timing != "AFTER" && timing != "BEFORE" {
		return empty, fmt.Errorf("%w: timing must be AFTER or BEFORE", ErrInvalidTrigger)
	}
	event := strings.ToUpper(strings.TrimSpace(definition.Event))
	if event != "INSERT" && event != "UPDATE" && event != "DELETE" {
		return empty, fmt.Errorf("%w: event must be INSERT, UPDATE or DELETE", ErrInvalidTrigger)
	}
	if len(definition.UpdateOf) > 64 || (len(definition.UpdateOf) != 0 && event != "UPDATE") {
		return empty, fmt.Errorf("%w: update_of is limited to 64 columns and only applies to UPDATE", ErrInvalidTrigger)
	}
	if strings.TrimSpace(definition.Body) == "" || len(definition.Body) > 64*1024 || strings.ContainsRune(definition.Body, 0) || len(definition.When) > 8*1024 || strings.ContainsRune(definition.When, 0) {
		return empty, fmt.Errorf("%w: provide a nonempty body up to 64 KiB and a condition up to 8 KiB, without NUL", ErrInvalidTrigger)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction; rollback is a fallback.
	var table, kind string
	err = tx.QueryRowContext(ctx, "SELECT name, type FROM pragma_table_list WHERE schema='main' AND name=? COLLATE NOCASE", definition.TableName).Scan(&table, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrTableNotFound
	}
	if err != nil {
		return empty, err
	}
	if kind != "table" {
		return empty, fmt.Errorf("%w: use an ordinary user table; views, virtual and shadow tables are not supported", ErrInvalidTrigger)
	}
	columns, err := triggerUpdateColumns(ctx, tx, table, definition.UpdateOf)
	if err != nil {
		return empty, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE name=? COLLATE NOCASE", definition.Name).Scan(&existing)
	if err == nil {
		return empty, ErrTriggerExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	header := fmt.Sprintf("CREATE TRIGGER %s %s %s", QuoteIdentifier(definition.Name), timing, event)
	if len(definition.UpdateOf) != 0 {
		header += " OF " + strings.Join(columns, ", ")
	}
	header += " ON " + QuoteIdentifier(table)
	if strings.TrimSpace(definition.When) != "" {
		header += "\nWHEN (" + definition.When + "\n)"
	}
	statement := header + "\nBEGIN\n" + definition.Body + "\nEND"
	// database/sql's prepared-statement path executes only the first SQLite statement;
	// ExecContext on the raw query instead would execute an injected tail as well.
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		return empty, triggerDefinitionError(err)
	}
	_, execErr := prepared.ExecContext(ctx)
	closeErr := prepared.Close()
	if execErr != nil {
		return empty, triggerDefinitionError(execErr)
	}
	if closeErr != nil {
		return empty, closeErr
	}
	trigger := domain.TriggerInfo{}
	err = tx.QueryRowContext(ctx, "SELECT name, tbl_name, sql FROM sqlite_master WHERE type='trigger' AND name=? COLLATE NOCASE", definition.Name).Scan(&trigger.Name, &trigger.TableName, &trigger.SQL)
	if err != nil {
		return empty, err
	}
	if trigger.SQL != statement {
		return empty, fmt.Errorf("%w: body and condition must not close the trigger or append statements outside it", ErrInvalidTrigger)
	}
	// Preparing a matching mutation compiles OLD/NEW references, conditions and body
	// tables/columns. EXPLAIN is never stepped and no sample row is written.
	validation := "EXPLAIN "
	switch event {
	case "INSERT":
		validation += "INSERT INTO " + QuoteIdentifier(table) + " DEFAULT VALUES"
	case "DELETE":
		validation += "DELETE FROM " + QuoteIdentifier(table) + " WHERE 0"
	case "UPDATE":
		assignments := make([]string, len(columns))
		for i, column := range columns {
			assignments[i] = column + "=" + column
		}
		validation += "UPDATE " + QuoteIdentifier(table) + " SET " + strings.Join(assignments, ", ") + " WHERE 0"
	}
	compiled, err := tx.PrepareContext(ctx, validation)
	if err != nil {
		return empty, triggerDefinitionError(err)
	}
	if err := compiled.Close(); err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return trigger, nil
}

func triggerDefinitionError(err error) error {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrError || sqliteErr.Code == sqlite3.ErrAuth) {
		return fmt.Errorf("%w: %s", ErrInvalidTrigger, err)
	}
	return err
}

// triggerUpdateColumns resolves UPDATE OF names against SQLite's catalog. SQLite
// silently accepts unknown UPDATE OF columns, so the API must reject them explicitly.
func triggerUpdateColumns(ctx context.Context, tx *sql.Tx, table string, selected []string) ([]string, error) {
	columns := make([]string, 0, len(selected))
	if len(selected) == 0 {
		rows, err := tx.QueryContext(ctx, "SELECT name FROM pragma_table_xinfo(?) WHERE hidden=0", table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				return nil, err
			}
			columns = append(columns, QuoteIdentifier(column))
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return columns, nil
	}
	seen := make(map[string]bool, len(selected))
	for _, column := range selected {
		if column == "" || strings.ContainsRune(column, 0) {
			return nil, fmt.Errorf("%w: update_of columns must not be empty or contain NUL", ErrInvalidTrigger)
		}
		var actual string
		var hidden int
		err := tx.QueryRowContext(ctx, "SELECT name, hidden FROM pragma_table_xinfo(?) WHERE name=? COLLATE NOCASE", table, column).Scan(&actual, &hidden)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: column %q does not exist", ErrInvalidTrigger, column)
		}
		if err != nil {
			return nil, err
		}
		if hidden != 0 || seen[actual] {
			return nil, fmt.Errorf("%w: update_of columns must be distinct and writable", ErrInvalidTrigger)
		}
		seen[actual] = true
		columns = append(columns, QuoteIdentifier(actual))
	}
	return columns, nil
}

// DropTrigger removes a custom trigger, including legacy quoted names and view
// triggers created through SQL. It never removes tables or existing records.
func DropTrigger(ctx context.Context, db *sql.DB, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || protectedSchemaName(name) {
		return "", fmt.Errorf("%w: choose a custom trigger without an internal prefix", ErrInvalidTrigger)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction; rollback is a fallback.
	var actual, table string
	err = tx.QueryRowContext(ctx, "SELECT name, tbl_name FROM sqlite_master WHERE type='trigger' AND name=? COLLATE NOCASE", name).Scan(&actual, &table)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTriggerNotFound
	}
	if err != nil {
		return "", err
	}
	if protectedSchemaName(table) {
		return "", fmt.Errorf("%w: triggers on internal tables cannot be removed", ErrInvalidTrigger)
	}
	var kind string
	if err := tx.QueryRowContext(ctx, "SELECT type FROM pragma_table_list WHERE schema='main' AND name=?", table).Scan(&kind); err != nil {
		return "", err
	}
	if kind != "table" && kind != "view" {
		return "", fmt.Errorf("%w: triggers on virtual or shadow tables cannot be removed", ErrInvalidTrigger)
	}
	if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+QuoteIdentifier(actual)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return actual, nil
}
