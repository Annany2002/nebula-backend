// internal/storage/userdb_repo.go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"github.com/Annany2002/nebula-backend/internal/core" // Import core for validation
	"github.com/mattn/go-sqlite3"
)

// Specific errors for user DB operations
var (
	ErrRecordNotFound      = errors.New("record not found")
	ErrTableNotFound       = errors.New("table not found")                   // Derived from specific error strings
	ErrColumnNotFound      = errors.New("column not found")                  // Derived
	ErrTypeMismatch        = errors.New("datatype mismatch")                 // Derived
	ErrConstraintViolation = errors.New("constraint violation")              // Derived
	ErrInvalidFilterValue  = errors.New("invalid value provided for filter") // New error
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

// --- *** NEW: List Tables in User DB *** ---

// ListTables retrieves a list of table names from the user's database file.
func ListTables(ctx context.Context, userDB *sql.DB) ([]string, error) {
	// Query sqlite_master (or sqlite_schema in newer versions) for tables
	// Exclude sqlite internal tables
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;`
	rows, err := userDB.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Storage: Error listing tables: %v", err)
		return nil, fmt.Errorf("database error listing tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Storage: Error scanning table name: %v", err)
			return nil, fmt.Errorf("failed processing table list: %w", err)
		}
		tableNames = append(tableNames, name)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Storage: Error iterating table list: %v", err)
		return nil, fmt.Errorf("failed reading table list: %w", err)
	}

	if tableNames == nil {
		tableNames = make([]string, 0)
	}
	return tableNames, nil
}

// --- *** END NEW *** ---

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

// Accepts tableName and query parameters directly.
func ListRecords(ctx context.Context, userDB *sql.DB, tableName string, queryParams url.Values) ([]map[string]any, error) {

	// 1. Fetch schema to validate filter keys and convert values
	columnTypes, err := PragmaTableInfo(ctx, userDB, tableName)
	if err != nil {
		return nil, err // Propagate ErrTableNotFound or other schema errors
	}

	// 2. Build WHERE clause and arguments from queryParams
	whereClauses := []string{}
	args := []any{}

	for key, values := range queryParams {
		if len(values) == 0 { // Should not happen with url.Values but check anyway
			continue
		}
		filterValueStr := values[0] // Use only the first value for simple equality filter
		lowerKey := strings.ToLower(key)

		// A. Validate filter key format
		if !core.IsValidIdentifier(key) {
			log.Printf("Storage: ListRecords received invalid filter key format: %s", key)
			// *** CHANGED: Return error instead of ignoring ***
			return nil, fmt.Errorf("%w: invalid filter key format '%s'", ErrInvalidFilterValue, key)
		}

		// B. Validate filter key exists in schema
		expectedType, exists := columnTypes[lowerKey]
		if !exists {
			log.Printf("Storage: ListRecords received filter key not in schema: %s", key)
			// *** CHANGED: Return error instead of ignoring ***
			return nil, fmt.Errorf("%w: filter key '%s' not found in table schema", ErrInvalidFilterValue, key)
		}

		// C. Attempt to convert filterValueStr to expected type (logic remains same)
		var convertedValue interface{}
		var conversionError error

		switch expectedType {
		case "INTEGER", "BOOLEAN": // Treat boolean as integer for filtering
			// Try parsing as int first
			if vInt, err := strconv.ParseInt(filterValueStr, 10, 64); err == nil {
				convertedValue = vInt
			} else {
				// Try parsing as bool (true/false -> 1/0) if original type was BOOLEAN
				// (Note: columnTypes stores normalized types like INTEGER, need original schema maybe?)
				// Let's keep it simple: if it's not a valid int, error out for INTEGER/BOOLEAN filter
				conversionError = fmt.Errorf("expected an integer for column '%s'", key)
			}
			// If handling actual boolean input:
			// else if expectedType == "BOOLEAN" { ... parse "true"/"false" ... }

		case "REAL":
			if vFloat, err := strconv.ParseFloat(filterValueStr, 64); err == nil {
				convertedValue = vFloat
			} else {
				conversionError = fmt.Errorf("expected a number (float) for column '%s'", key)
			}
		case "TEXT":
			convertedValue = filterValueStr // Keep as string
		case "BLOB":
			// Cannot reliably filter BLOBs with simple equality from URL param
			log.Printf("Storage: ListRecords ignoring filter on BLOB column: %s", key)
			continue // Skip BLOB filtering
		default:
			log.Printf("Storage: ListRecords ignoring filter on column '%s' with unhandled type '%s'", key, expectedType)
			continue // Skip unknown types
		}

		if conversionError != nil {
			log.Printf("Storage: ListRecords conversion error for key '%s', value '%s': %v", key, filterValueStr, conversionError)
			return nil, fmt.Errorf("%w: %s", ErrInvalidFilterValue, conversionError.Error()) // Return specific error
		}

		// C. Add to WHERE clause and arguments
		// Use original key case from query param for the SQL column name for clarity
		whereClauses = append(whereClauses, fmt.Sprintf("%s = ?", key))
		args = append(args, convertedValue)

	} // End loop over queryParams

	// 3. Construct final SQL
	selectSQL := fmt.Sprintf("SELECT * FROM %s", tableName) // tableName validated by handler implicitly via path lookup
	if len(whereClauses) > 0 {
		selectSQL += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	selectSQL += ";" // End statement

	log.Printf("Storage: Executing List Records SQL: %s | Args: %v", selectSQL, args)

	// 4. Execute query
	rows, err := userDB.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		log.Printf("Storage: Failed filtered SELECT *: %v\nSQL: %s", err, selectSQL)
		// No need to check "no such table" here, PragmaTableInfo already did
		return nil, fmt.Errorf("database error listing records: %w", err)
	}
	defer rows.Close()

	// 5. Process results (same as before)
	columns, err := rows.Columns()
	if err != nil { /* ... handle error ... */
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
		if err := rows.Scan(scanArgs...); err != nil { /* ... handle scan error ... */
			return nil, fmt.Errorf("failed reading record data: %w", err)
		}

		rowData := make(map[string]interface{})
		for i, colName := range columns {
			rawValue := values[i]
			if byteSlice, ok := rawValue.([]byte); ok {
				rowData[colName] = string(byteSlice)
			} else {
				rowData[colName] = rawValue
			}
		}
		results = append(results, rowData)
	}
	if err = rows.Err(); err != nil { /* ... handle iteration error ... */
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
