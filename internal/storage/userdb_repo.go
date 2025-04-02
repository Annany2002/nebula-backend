// internal/storage/userdb_repo.go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// Specific errors for user DB operations
var (
	ErrRecordNotFound      = errors.New("record not found")
	ErrTableNotFound       = errors.New("table not found")      // Derived from specific error strings
	ErrColumnNotFound      = errors.New("column not found")     // Derived
	ErrTypeMismatch        = errors.New("datatype mismatch")    // Derived
	ErrConstraintViolation = errors.New("constraint violation") // Derived
)

// --- User DB Connection ---

// ConnectUserDB opens and pings a connection to a specific user DB file.
// The caller is responsible for closing the connection.
func ConnectUserDB(ctx context.Context, filePath string) (*sql.DB, error) {
	log.Printf("Storage: Opening user DB: %s", filePath)
	// Ensure foreign keys and consider WAL mode for better concurrency if needed
	userDB, err := sql.Open("sqlite3", filePath+"?_foreign_keys=on")
	if err != nil {
		log.Printf("Storage: Failed to open user DB file '%s': %v", filePath, err)
		return nil, fmt.Errorf("failed to access user database storage: %w", err)
	}

	// Ping to verify connection
	if err = userDB.PingContext(ctx); err != nil {
		userDB.Close() // Close if ping fails
		log.Printf("Storage: Failed to ping user DB '%s': %v", filePath, err)
		return nil, fmt.Errorf("failed to connect to user database storage: %w", err)
	}

	// Optional: Configure pool for user DB connections if kept open longer
	// userDB.SetMaxOpenConns(5)
	// userDB.SetMaxIdleConns(1)

	return userDB, nil
}

// --- User DB Schema Operations ---

// PragmaTableInfo retrieves schema information for a table.
func PragmaTableInfo(ctx context.Context, userDB *sql.DB, tableName string) (map[string]string, error) {
	pragmaSQL := fmt.Sprintf("PRAGMA table_info(%s);", tableName) // Assumes tableName is pre-validated
	rows, err := userDB.QueryContext(ctx, pragmaSQL)
	if err != nil {
		log.Printf("Storage: Failed PRAGMA for Table '%s': %v", tableName, err)
		// Check if error indicates table not found
		if strings.Contains(err.Error(), "no such table") { // Brittle check
			return nil, ErrTableNotFound
		}
		return nil, fmt.Errorf("failed to retrieve schema: %w", err)
	}
	defer rows.Close()

	columnTypes := make(map[string]string)
	foundColumns := false
	for rows.Next() {
		foundColumns = true
		var cid int
		var name string
		var sqlType string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &sqlType, &notnull, &dfltValue, &pk); err != nil {
			log.Printf("Storage: Failed scanning PRAGMA for Table '%s': %v", tableName, err)
			return nil, fmt.Errorf("failed to parse schema: %w", err)
		}
		columnTypes[strings.ToLower(name)] = strings.ToUpper(sqlType)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Storage: Error iterating PRAGMA for Table '%s': %v", tableName, err)
		return nil, fmt.Errorf("failed to read schema: %w", err)
	}
	if !foundColumns {
		return nil, ErrTableNotFound // No rows means table doesn't exist
	}
	return columnTypes, nil
}

// CreateTable executes a CREATE TABLE statement in the user DB.
func CreateTable(ctx context.Context, userDB *sql.DB, createSQL string) error {
	_, err := userDB.ExecContext(ctx, createSQL) // createSQL assumed pre-validated
	if err != nil {
		log.Printf("Storage: Failed to execute CREATE TABLE: %v\nSQL: %s", err, createSQL)
		// Could try to parse error for specific issues (e.g., table exists if not using IF NOT EXISTS)
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

// --- User DB Record CRUD Operations ---

// InsertRecord executes an INSERT statement and returns the last insert ID.
func InsertRecord(ctx context.Context, userDB *sql.DB, insertSQL string, values ...interface{}) (int64, error) {
	result, err := userDB.ExecContext(ctx, insertSQL, values...)
	if err != nil {
		log.Printf("Storage: Failed INSERT: %v\nSQL: %s", err, insertSQL)
		// Map common SQLite errors to specific storage errors
		if strings.Contains(err.Error(), "no such table") {
			return 0, ErrTableNotFound
		}
		if strings.Contains(err.Error(), "has no column named") {
			return 0, ErrColumnNotFound
		}
		if strings.Contains(err.Error(), "datatype mismatch") {
			return 0, ErrTypeMismatch
		}
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			return 0, ErrConstraintViolation
		}
		return 0, fmt.Errorf("database error during insert: %w", err)
	}
	lastID, err := result.LastInsertId()
	if err != nil {
		log.Printf("Storage: Failed to get LastInsertId after INSERT: %v", err)
		return 0, fmt.Errorf("failed to retrieve ID after insert: %w", err)
	}
	return lastID, nil
}

// ListRecords executes SELECT * and returns results as a slice of maps.
func ListRecords(ctx context.Context, userDB *sql.DB, selectSQL string) ([]map[string]interface{}, error) {
	rows, err := userDB.QueryContext(ctx, selectSQL) // Assumes selectSQL is safe
	if err != nil {
		log.Printf("Storage: Failed SELECT *: %v\nSQL: %s", err, selectSQL)
		if strings.Contains(err.Error(), "no such table") {
			return nil, ErrTableNotFound
		}
		return nil, fmt.Errorf("database error listing records: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		log.Printf("Storage: Failed getting columns for SELECT *: %v", err)
		return nil, fmt.Errorf("failed processing results: %w", err)
	}
	numColumns := len(columns)
	results := make([]map[string]interface{}, 0)

	for rows.Next() {
		scanArgs := make([]interface{}, numColumns)
		values := make([]interface{}, numColumns)
		for i := range values {
			scanArgs[i] = &values[i]
		}

		if err := rows.Scan(scanArgs...); err != nil {
			log.Printf("Storage: Failed scanning row for SELECT *: %v", err)
			return nil, fmt.Errorf("failed reading record data: %w", err)
		}

		rowData := make(map[string]interface{})
		for i, colName := range columns {
			rawValue := values[i]
			if byteSlice, ok := rawValue.([]byte); ok {
				rowData[colName] = string(byteSlice) // Handle bytes -> string
			} else {
				rowData[colName] = rawValue
			}
		}
		results = append(results, rowData)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Storage: Error during iteration for SELECT *: %v", err)
		return nil, fmt.Errorf("failed processing all records: %w", err)
	}
	return results, nil
}

// GetRecord executes SELECT * WHERE id = ? and returns a single map or ErrRecordNotFound.
func GetRecord(ctx context.Context, userDB *sql.DB, selectSQL string, recordID int64) (map[string]interface{}, error) {
	rows, err := userDB.QueryContext(ctx, selectSQL, recordID) // selectSQL assumed safe with placeholder
	if err != nil {
		log.Printf("Storage: Failed SELECT by ID: %v\nSQL: %s", err, selectSQL)
		if strings.Contains(err.Error(), "no such table") {
			return nil, ErrTableNotFound
		}
		return nil, fmt.Errorf("database error getting record: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil { /* ... handle ... */
		return nil, fmt.Errorf("failed processing results: %w", err)
	}
	numColumns := len(columns)

	if !rows.Next() { // Check if a row exists
		if err = rows.Err(); err != nil { /* ... handle iteration error ... */
			return nil, fmt.Errorf("failed checking for record: %w", err)
		}
		return nil, ErrRecordNotFound // No row found
	}

	scanArgs := make([]interface{}, numColumns)
	values := make([]interface{}, numColumns)
	for i := range values {
		scanArgs[i] = &values[i]
	}

	if err := rows.Scan(scanArgs...); err != nil {
		log.Printf("Storage: Failed scanning row for SELECT by ID: %v", err)
		return nil, fmt.Errorf("failed reading record data: %w", err)
	}

	// Process row into map
	rowData := make(map[string]interface{})
	for i, colName := range columns {
		rawValue := values[i]
		if byteSlice, ok := rawValue.([]byte); ok {
			rowData[colName] = string(byteSlice)
		} else {
			rowData[colName] = rawValue
		}
	}

	// Ensure no more rows (optional check)
	// if rows.Next() { log.Printf("WARN: Found multiple rows for ID %d", recordID) }

	return rowData, nil
}

// UpdateRecord executes an UPDATE statement and returns rows affected.
func UpdateRecord(ctx context.Context, userDB *sql.DB, updateSQL string, values ...interface{}) (int64, error) {
	result, err := userDB.ExecContext(ctx, updateSQL, values...)
	if err != nil {
		log.Printf("Storage: Failed UPDATE: %v\nSQL: %s", err, updateSQL)
		if strings.Contains(err.Error(), "no such table") {
			return 0, ErrTableNotFound
		}
		if strings.Contains(err.Error(), "no such column") {
			return 0, ErrColumnNotFound
		} // Less likely due to PRAGMA check
		if strings.Contains(err.Error(), "datatype mismatch") {
			return 0, ErrTypeMismatch
		} // Less likely
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			return 0, ErrConstraintViolation
		}
		return 0, fmt.Errorf("database error during update: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Storage: Failed getting RowsAffected after UPDATE: %v", err)
		return 0, fmt.Errorf("failed confirming update: %w", err)
	}
	if rowsAffected == 0 {
		return 0, ErrRecordNotFound // No rows matched the WHERE clause
	}
	return rowsAffected, nil
}

// DeleteRecord executes a DELETE statement and returns rows affected.
func DeleteRecord(ctx context.Context, userDB *sql.DB, deleteSQL string, recordID int64) (int64, error) {
	result, err := userDB.ExecContext(ctx, deleteSQL, recordID) // deleteSQL assumed safe with placeholder
	if err != nil {
		log.Printf("Storage: Failed DELETE: %v\nSQL: %s", err, deleteSQL)
		// Less likely to get specific errors here, maybe just connection issues
		return 0, fmt.Errorf("database error during delete: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Storage: Failed getting RowsAffected after DELETE: %v", err)
		return 0, fmt.Errorf("failed confirming delete: %w", err)
	}
	if rowsAffected == 0 {
		return 0, ErrRecordNotFound // No rows matched the WHERE clause
	}
	return rowsAffected, nil
}
