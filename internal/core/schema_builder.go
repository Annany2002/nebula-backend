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
