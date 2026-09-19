// internal/core/validation.go
package core

import (
	"regexp"
	"strings"
)

// Regular expression for valid database/table/column names (alphanumeric + underscore)
var nameValidationRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Allowed SQLite column types for user definition (uppercase keys and values)
var AllowedColumnTypes = map[string]string{
	"TEXT":     "TEXT",
	"INTEGER":  "INTEGER",
	"REAL":     "REAL",
	"BLOB":     "BLOB",
	"BOOLEAN":  "BOOLEAN", // Represented as INTEGER in SQLite usually
	"DATETIME": "DATETIME",
	"NUMERIC":  "NUMERIC",
}

// IsValidIdentifier checks if a string is a valid identifier (e.g., db_name, table_name, column_name)
// Applies basic format and length checks.
func IsValidIdentifier(name string) bool {
	// Consider adding min length if needed, adjust max length as appropriate
	return nameValidationRegex.MatchString(name) && name != "" && len(name) <= 64
}

// NormalizeAndValidateType checks if a string is an allowed column type, returning the normalized uppercase version.
func NormalizeAndValidateType(colType string) (string, bool) {
	upperType := strings.ToUpper(colType)
	normalizedType, ok := AllowedColumnTypes[upperType]
	// Could add extra checks here if needed (e.g., disallow specific types in certain contexts)
	return normalizedType, ok
}

// Allowed SQLite foreign key actions
var AllowedForeignKeyActions = map[string]string{
	"CASCADE":     "CASCADE",
	"SET NULL":    "SET NULL",
	"SET DEFAULT": "SET DEFAULT",
	"RESTRICT":    "RESTRICT",
	"NO ACTION":   "NO ACTION",
}

// NormalizeForeignKeyAction validates and normalizes an ON DELETE or ON UPDATE action.
// Returns empty string and true if action is empty (defaults to SQLite default).
func NormalizeForeignKeyAction(action string) (string, bool) {
	trimmed := strings.TrimSpace(action)
	if trimmed == "" {
		return "", true
	}
	normalized, ok := AllowedForeignKeyActions[strings.ToUpper(trimmed)]
	return normalized, ok
}
