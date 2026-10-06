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
	ErrInvalidIndex  = errors.New("invalid index definition")
	ErrIndexExists   = errors.New("index name is already in use")
	ErrIndexNotFound = errors.New("index not found")
)

func protectedSchemaName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "sqlite_") || strings.HasPrefix(lower, "_nebula_")
}

// CreateIndex validates against the live table catalog and creates the index atomically.
// It never replaces an existing object, including when a caller repeats the same request.
func CreateIndex(ctx context.Context, db *sql.DB, name, table string, columns []string, unique bool) (domain.IndexInfo, error) {
	if !core.IsValidIdentifier(name) || protectedSchemaName(name) {
		return domain.IndexInfo{}, fmt.Errorf("%w: name must contain 1–64 letters, digits or underscores without an internal prefix", ErrInvalidIndex)
	}
	if table == "" || strings.ContainsRune(table, 0) || protectedSchemaName(table) || len(columns) == 0 || len(columns) > 64 {
		return domain.IndexInfo{}, fmt.Errorf("%w: choose a user table and between 1 and 64 columns", ErrInvalidIndex)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.IndexInfo{}, err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction; rollback is a fallback.
	var actualTable, tableKind string
	err = tx.QueryRowContext(ctx, "SELECT name, type FROM pragma_table_list WHERE schema='main' AND name=? COLLATE NOCASE", table).Scan(&actualTable, &tableKind)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && tableKind == "view") {
		return domain.IndexInfo{}, ErrTableNotFound
	}
	if err != nil {
		return domain.IndexInfo{}, err
	}
	if tableKind != "table" {
		return domain.IndexInfo{}, fmt.Errorf("%w: virtual and shadow tables do not support custom indexes", ErrInvalidIndex)
	}
	quotedColumns := make([]string, 0, len(columns))
	seen := make(map[string]bool, len(columns))
	for _, column := range columns {
		if column == "" || strings.ContainsRune(column, 0) {
			return domain.IndexInfo{}, fmt.Errorf("%w: column names must not be empty or contain NUL", ErrInvalidIndex)
		}
		var actualColumn string
		err = tx.QueryRowContext(ctx, "SELECT name FROM pragma_table_xinfo(?) WHERE name=? COLLATE NOCASE", actualTable, column).Scan(&actualColumn)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.IndexInfo{}, fmt.Errorf("%w: column %q does not exist", ErrInvalidIndex, column)
		}
		if err != nil {
			return domain.IndexInfo{}, err
		}
		if seen[actualColumn] {
			return domain.IndexInfo{}, fmt.Errorf("%w: column %q is repeated", ErrInvalidIndex, column)
		}
		seen[actualColumn] = true
		quotedColumns = append(quotedColumns, QuoteIdentifier(actualColumn))
	}
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE name=? COLLATE NOCASE", name).Scan(&existing)
	if err == nil {
		return domain.IndexInfo{}, ErrIndexExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.IndexInfo{}, err
	}
	kind := "INDEX"
	if unique {
		kind = "UNIQUE INDEX"
	}
	statement := fmt.Sprintf("CREATE %s %s ON %s (%s)", kind, QuoteIdentifier(name), QuoteIdentifier(actualTable), strings.Join(quotedColumns, ", "))
	if _, err = tx.ExecContext(ctx, statement); err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			return domain.IndexInfo{}, fmt.Errorf("%w: existing rows do not satisfy the unique index", ErrConstraintViolation)
		}
		return domain.IndexInfo{}, err
	}
	index := domain.IndexInfo{Name: name, TableName: actualTable, Unique: unique, SQL: statement}
	if err := tx.Commit(); err != nil {
		return domain.IndexInfo{}, err
	}
	return index, nil
}

// DropIndex removes only an explicit index on a user table. SQLite constraint indexes
// and Nebula metadata indexes cannot be removed through this endpoint.
func DropIndex(ctx context.Context, db *sql.DB, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || protectedSchemaName(name) {
		return "", fmt.Errorf("%w: choose a custom index without an internal prefix", ErrInvalidIndex)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction; rollback is a fallback.
	var actualName, table string
	var definition sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT name, tbl_name, sql FROM sqlite_master WHERE type='index' AND name=? COLLATE NOCASE", name).Scan(&actualName, &table, &definition)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrIndexNotFound
	}
	if err != nil {
		return "", err
	}
	if !definition.Valid || protectedSchemaName(table) {
		return "", fmt.Errorf("%w: internal or constraint indexes cannot be removed", ErrInvalidIndex)
	}
	var tableKind string
	if err := tx.QueryRowContext(ctx, "SELECT type FROM pragma_table_list WHERE schema='main' AND name=?", table).Scan(&tableKind); err != nil {
		return "", err
	}
	if tableKind != "table" {
		return "", fmt.Errorf("%w: indexes on virtual or shadow tables cannot be removed", ErrInvalidIndex)
	}
	if _, err = tx.ExecContext(ctx, "DROP INDEX "+QuoteIdentifier(actualName)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return actualName, nil
}
