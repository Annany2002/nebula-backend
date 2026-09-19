package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Annany2002/nebula-backend/api/models"
)

// BuildCreateTableSQL validates the schema request and generates a SQLite CREATE TABLE statement.
// It supports primary key auto-increment, standard column types, timestamp creation,
// and foreign key constraints (both column-level and table-level).
func BuildCreateTableSQL(req *models.CreateSchemaRequest) (string, error) {
	if req == nil {
		return "", errors.New("schema request cannot be nil")
	}

	trimmedTableName := strings.TrimSpace(req.TableName)
	if !IsValidIdentifier(trimmedTableName) {
		return "", fmt.Errorf("invalid table name '%s': must be alphanumeric/underscore and <= 64 characters", req.TableName)
	}

	// Columns can be in req.Columns or req.Schema
	cols := req.Columns
	if len(cols) == 0 {
		cols = req.Schema
	}
	if len(cols) == 0 {
		return "", errors.New("at least one column definition is required")
	}

	columnDefs := make([]string, 0, len(cols))
	columnNames := make(map[string]bool, len(cols))
	var fkConstraints []string

	for _, col := range cols {
		colName := strings.TrimSpace(col.Name)
		colNameLower := strings.ToLower(colName)

		if !IsValidIdentifier(colName) {
			return "", fmt.Errorf("invalid column name '%s': must be alphanumeric/underscore and <= 64 characters", col.Name)
		}
		if colNameLower == "id" {
			return "", errors.New("column name 'id' is reserved for the auto-increment primary key")
		}
		if colNameLower == "created_at" {
			return "", errors.New("column name 'created_at' is reserved for creation timestamp")
		}
		if columnNames[colNameLower] {
			return "", fmt.Errorf("duplicate column name '%s'", col.Name)
		}
		columnNames[colNameLower] = true

		normalizedType, ok := NormalizeAndValidateType(col.Type)
		if !ok {
			return "", fmt.Errorf("invalid type '%s' for column '%s'", col.Type, col.Name)
		}

		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", colName, normalizedType))

		// Process column-level foreign key
		if col.ForeignKey != nil {
			fkSQL, err := buildForeignKeyClause(colName, col.ForeignKey.TargetTable, col.ForeignKey.TargetColumn, col.ForeignKey.OnDelete, col.ForeignKey.OnUpdate)
			if err != nil {
				return "", fmt.Errorf("invalid foreign key for column '%s': %w", col.Name, err)
			}
			fkConstraints = append(fkConstraints, fkSQL)
		}
	}

	// Process table-level foreign keys
	for _, fk := range req.ForeignKeys {
		colName := strings.TrimSpace(fk.Column)
		if !columnNames[strings.ToLower(colName)] {
			return "", fmt.Errorf("foreign key references unknown column '%s' in table '%s'", fk.Column, trimmedTableName)
		}
		fkSQL, err := buildForeignKeyClause(colName, fk.TargetTable, fk.TargetColumn, fk.OnDelete, fk.OnUpdate)
		if err != nil {
			return "", fmt.Errorf("invalid table-level foreign key: %w", err)
		}
		fkConstraints = append(fkConstraints, fkSQL)
	}

	allElements := make([]string, 0, 1+len(columnDefs)+1+len(fkConstraints))
	allElements = append(allElements, "id INTEGER PRIMARY KEY AUTOINCREMENT")
	allElements = append(allElements, columnDefs...)
	allElements = append(allElements, "created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP")
	allElements = append(allElements, fkConstraints...)

	createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n    %s\n);",
		trimmedTableName,
		strings.Join(allElements, ",\n    "),
	)

	return createTableSQL, nil
}

func buildForeignKeyClause(sourceCol, targetTable, targetCol, onDelete, onUpdate string) (string, error) {
	trimmedTargetTable := strings.TrimSpace(targetTable)
	trimmedTargetCol := strings.TrimSpace(targetCol)

	if !IsValidIdentifier(trimmedTargetTable) {
		return "", fmt.Errorf("invalid target table '%s'", targetTable)
	}
	if !IsValidIdentifier(trimmedTargetCol) {
		return "", fmt.Errorf("invalid target column '%s'", targetCol)
	}

	clause := fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s)", sourceCol, trimmedTargetTable, trimmedTargetCol)

	if onDelete != "" {
		normalizedAction, ok := NormalizeForeignKeyAction(onDelete)
		if !ok {
			return "", fmt.Errorf("invalid ON DELETE action '%s'", onDelete)
		}
		if normalizedAction != "" {
			clause += fmt.Sprintf(" ON DELETE %s", normalizedAction)
		}
	}

	if onUpdate != "" {
		normalizedAction, ok := NormalizeForeignKeyAction(onUpdate)
		if !ok {
			return "", fmt.Errorf("invalid ON UPDATE action '%s'", onUpdate)
		}
		if normalizedAction != "" {
			clause += fmt.Sprintf(" ON UPDATE %s", normalizedAction)
		}
	}

	return clause, nil
}

// BuildAddColumnSQL generates the ALTER TABLE ... ADD COLUMN statement.
func BuildAddColumnSQL(tableName string, col *models.AlterColumnDefinition) (string, error) {
	if col == nil {
		return "", errors.New("column definition cannot be nil")
	}
	trimmedTable := strings.TrimSpace(tableName)
	if !IsValidIdentifier(trimmedTable) {
		return "", fmt.Errorf("invalid table name '%s'", tableName)
	}
	colName := strings.TrimSpace(col.Name)
	if !IsValidIdentifier(colName) {
		return "", fmt.Errorf("invalid column name '%s'", col.Name)
	}
	colLower := strings.ToLower(colName)
	if colLower == "id" {
		return "", errors.New("column name 'id' is reserved for the primary key")
	}
	if colLower == "created_at" {
		return "", errors.New("column name 'created_at' is reserved for creation timestamp")
	}
	normalizedType, ok := NormalizeAndValidateType(col.Type)
	if !ok {
		return "", fmt.Errorf("invalid type '%s' for column '%s'", col.Type, col.Name)
	}

	parts := []string{fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", trimmedTable, colName, normalizedType)}

	if col.NotNull {
		if col.DefaultValue == nil || *col.DefaultValue == "" {
			return "", fmt.Errorf("column '%s' has NOT NULL constraint but no DEFAULT value specified", colName)
		}
		parts = append(parts, "NOT NULL")
	}

	if col.DefaultValue != nil && *col.DefaultValue != "" {
		parts = append(parts, fmt.Sprintf("DEFAULT %s", *col.DefaultValue))
	}

	if col.ForeignKey != nil {
		refClause, err := buildColumnReferenceClause(col.ForeignKey.TargetTable, col.ForeignKey.TargetColumn, col.ForeignKey.OnDelete, col.ForeignKey.OnUpdate)
		if err != nil {
			return "", fmt.Errorf("invalid foreign key for column '%s': %w", colName, err)
		}
		parts = append(parts, refClause)
	}

	return strings.Join(parts, " ") + ";", nil
}

func buildColumnReferenceClause(targetTable, targetCol, onDelete, onUpdate string) (string, error) {
	trimmedTargetTable := strings.TrimSpace(targetTable)
	trimmedTargetCol := strings.TrimSpace(targetCol)

	if !IsValidIdentifier(trimmedTargetTable) {
		return "", fmt.Errorf("invalid target table '%s'", targetTable)
	}
	if !IsValidIdentifier(trimmedTargetCol) {
		return "", fmt.Errorf("invalid target column '%s'", targetCol)
	}

	clause := fmt.Sprintf("REFERENCES %s(%s)", trimmedTargetTable, trimmedTargetCol)

	if onDelete != "" {
		normalizedAction, ok := NormalizeForeignKeyAction(onDelete)
		if !ok {
			return "", fmt.Errorf("invalid ON DELETE action '%s'", onDelete)
		}
		if normalizedAction != "" {
			clause += fmt.Sprintf(" ON DELETE %s", normalizedAction)
		}
	}

	if onUpdate != "" {
		normalizedAction, ok := NormalizeForeignKeyAction(onUpdate)
		if !ok {
			return "", fmt.Errorf("invalid ON UPDATE action '%s'", onUpdate)
		}
		if normalizedAction != "" {
			clause += fmt.Sprintf(" ON UPDATE %s", normalizedAction)
		}
	}

	return clause, nil
}

// BuildDropColumnSQL generates the ALTER TABLE ... DROP COLUMN statement.
func BuildDropColumnSQL(tableName, columnName string) (string, error) {
	trimmedTable := strings.TrimSpace(tableName)
	if !IsValidIdentifier(trimmedTable) {
		return "", fmt.Errorf("invalid table name '%s'", tableName)
	}
	colName := strings.TrimSpace(columnName)
	if !IsValidIdentifier(colName) {
		return "", fmt.Errorf("invalid column name '%s'", columnName)
	}
	colLower := strings.ToLower(colName)
	if colLower == "id" {
		return "", errors.New("cannot drop reserved primary key column 'id'")
	}
	if colLower == "created_at" {
		return "", errors.New("cannot drop reserved timestamp column 'created_at'")
	}

	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", trimmedTable, colName), nil
}

// BuildRenameColumnSQL generates the ALTER TABLE ... RENAME COLUMN statement.
func BuildRenameColumnSQL(tableName, oldName, newName string) (string, error) {
	trimmedTable := strings.TrimSpace(tableName)
	if !IsValidIdentifier(trimmedTable) {
		return "", fmt.Errorf("invalid table name '%s'", tableName)
	}
	oldTrimmed := strings.TrimSpace(oldName)
	newTrimmed := strings.TrimSpace(newName)
	if !IsValidIdentifier(oldTrimmed) {
		return "", fmt.Errorf("invalid existing column name '%s'", oldName)
	}
	if !IsValidIdentifier(newTrimmed) {
		return "", fmt.Errorf("invalid new column name '%s'", newName)
	}
	if strings.EqualFold(oldTrimmed, newTrimmed) {
		return "", errors.New("new column name must be different from current column name")
	}
	if strings.ToLower(oldTrimmed) == "id" || strings.ToLower(newTrimmed) == "id" {
		return "", errors.New("cannot rename reserved primary key column 'id'")
	}
	if strings.ToLower(oldTrimmed) == "created_at" || strings.ToLower(newTrimmed) == "created_at" {
		return "", errors.New("cannot rename reserved timestamp column 'created_at'")
	}

	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", trimmedTable, oldTrimmed, newTrimmed), nil
}

// BuildRenameTableSQL generates the ALTER TABLE ... RENAME TO statement.
func BuildRenameTableSQL(oldTableName, newTableName string) (string, error) {
	oldTrimmed := strings.TrimSpace(oldTableName)
	newTrimmed := strings.TrimSpace(newTableName)
	if !IsValidIdentifier(oldTrimmed) {
		return "", fmt.Errorf("invalid existing table name '%s'", oldTableName)
	}
	if !IsValidIdentifier(newTrimmed) {
		return "", fmt.Errorf("invalid new table name '%s'", newTableName)
	}
	if strings.EqualFold(oldTrimmed, newTrimmed) {
		return "", errors.New("new table name must be different from current table name")
	}

	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", oldTrimmed, newTrimmed), nil
}

// BuildAlterTableStatements generates and validates SQL statements for all operations in the request.
func BuildAlterTableStatements(tableName string, req *models.AlterTableRequest) ([]string, string, error) {
	if req == nil {
		return nil, "", errors.New("alter table request cannot be nil")
	}
	ops := req.GetOperations()
	if len(ops) == 0 {
		return nil, "", errors.New("at least one alteration operation is required")
	}

	currentTable := tableName
	var sqlStatements []string

	for idx, op := range ops {
		action := strings.ToLower(strings.TrimSpace(op.Action))
		switch action {
		case "add_column":
			sql, err := BuildAddColumnSQL(currentTable, op.Column)
			if err != nil {
				return nil, "", fmt.Errorf("operation %d (%s): %w", idx+1, action, err)
			}
			sqlStatements = append(sqlStatements, sql)

		case "drop_column":
			sql, err := BuildDropColumnSQL(currentTable, op.ColumnName)
			if err != nil {
				return nil, "", fmt.Errorf("operation %d (%s): %w", idx+1, action, err)
			}
			sqlStatements = append(sqlStatements, sql)

		case "rename_column":
			sql, err := BuildRenameColumnSQL(currentTable, op.OldName, op.NewName)
			if err != nil {
				return nil, "", fmt.Errorf("operation %d (%s): %w", idx+1, action, err)
			}
			sqlStatements = append(sqlStatements, sql)

		case "rename_table":
			sql, err := BuildRenameTableSQL(currentTable, op.NewTableName)
			if err != nil {
				return nil, "", fmt.Errorf("operation %d (%s): %w", idx+1, action, err)
			}
			sqlStatements = append(sqlStatements, sql)
			currentTable = strings.TrimSpace(op.NewTableName)

		default:
			return nil, "", fmt.Errorf("operation %d: unsupported action '%s'", idx+1, op.Action)
		}
	}

	return sqlStatements, currentTable, nil
}
