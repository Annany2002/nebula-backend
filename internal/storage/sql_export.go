package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ExportDatabaseSQL reads schema and rows from a single SQLite transaction.
func ExportDatabaseSQL(ctx context.Context, db *sql.DB) (string, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck
	rows, err := tx.QueryContext(ctx, `SELECT name,sql FROM sqlite_master
 WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_nebula_%' ORDER BY name`)
	if err != nil {
		return "", err
	}
	type tableDefinition struct{ name, ddl string }
	var tables []tableDefinition
	for rows.Next() {
		var table tableDefinition
		if err := rows.Scan(&table.name, &table.ddl); err != nil {
			rows.Close()
			return "", err
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	var dump strings.Builder
	fmt.Fprintf(&dump, "-- Nebula SQLite Database Dump\n-- Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))
	dump.WriteString("PRAGMA foreign_keys=OFF;\nBEGIN TRANSACTION;\n\n")
	for _, table := range tables {
		fmt.Fprintf(&dump, "%s;\n", table.ddl)
		if err := exportTableRows(ctx, tx, table.name, &dump); err != nil {
			return "", fmt.Errorf("export table %q: %w", table.name, err)
		}
	}
	// Create dependent objects after loading data so triggers do not run during restore.
	rows, err = tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE type IN ('index','view','trigger')
 AND sql IS NOT NULL AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_nebula_%'
 ORDER BY CASE type WHEN 'index' THEN 0 WHEN 'view' THEN 1 ELSE 2 END,name`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			rows.Close()
			return "", err
		}
		fmt.Fprintf(&dump, "%s;\n", ddl)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	dump.WriteString("COMMIT;\nPRAGMA foreign_keys=ON;\n")
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return dump.String(), nil
}

func exportTableRows(ctx context.Context, tx *sql.Tx, name string, dump *strings.Builder) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+QuoteIdentifier(name)+")")
	if err != nil {
		return err
	}
	var columns []string
	for rows.Next() {
		var cid, notNull, pk int
		var column, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		columns = append(columns, QuoteIdentifier(column))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	literals := make([]string, len(columns))
	for i, column := range columns {
		// SQLite quote() preserves storage types; embedded NUL text needs a hex literal.
		literals[i] = fmt.Sprintf(`CASE WHEN typeof(%[1]s)='text' AND instr(%[1]s,char(0))>0
   THEN 'CAST(X''' || hex(%[1]s) || ''' AS TEXT)' ELSE quote(%[1]s) END`, column)
	}
	// Schema identifiers are double-quoted; literals contain only fixed SQL and quoted columns.
	rows, err = tx.QueryContext(ctx, "SELECT "+strings.Join(literals, ",")+" FROM "+QuoteIdentifier(name)) //nolint:gosec // Identifiers cannot be bound as parameters and are escaped with QuoteIdentifier.
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		values := make([]string, len(columns))
		arguments := make([]any, len(columns))
		for i := range values {
			arguments[i] = &values[i]
		}
		if err := rows.Scan(arguments...); err != nil {
			return err
		}
		fmt.Fprintf(dump, "INSERT INTO %s (%s) VALUES (%s);\n", QuoteIdentifier(name), strings.Join(columns, ","), strings.Join(values, ","))
	}
	return rows.Err()
}
