// internal/storage/userdb_repo.go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mattn/go-sqlite3"

	"github.com/Annany2002/nebula-backend/internal/core" // Import core for validation
	"github.com/Annany2002/nebula-backend/internal/domain"
)

// Specific errors for user DB operations
var (
	ErrRecordNotFound      = errors.New("record not found")
	ErrTableNotFound       = errors.New("table not found")                   // Derived from specific error strings
	ErrColumnNotFound      = errors.New("column not found")                  // Derived
	ErrTypeMismatch        = errors.New("datatype mismatch")                 // Derived
	ErrConstraintViolation = errors.New("constraint violation")              // Derived
	ErrInvalidFilterValue  = errors.New("invalid value provided for filter") // New error
	ErrInvalidSortColumn   = errors.New("invalid sort column")
	ErrInvalidFieldColumn  = errors.New("invalid field column")
)

// ListRecordsResult contains records and pagination metadata
type ListRecordsResult struct {
	Records    []map[string]any `json:"records"`
	Pagination PaginationMeta   `json:"pagination"`
}

// PaginationMeta contains pagination information
type PaginationMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// --- User DB Connection ---

// ConnectUserDB opens and pings a connection to a specific user DB file.
// The caller is responsible for closing the connection.
func ConnectUserDB(ctx context.Context, filePath string) (*sql.DB, error) {
	customLog.Printf("Storage: Opening user DB: %s", filePath)
	// Ensured foreign keys, WAL mode and busy timeout for better concurrency
	userDb, err := sql.Open("sqlite3", filePath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		customLog.Warnf("Storage: Failed to open user DB file '%s': %v", filePath, err)
		return nil, fmt.Errorf("failed to access user database storage: %w", err)
	}

	// Ping to verify connection
	if err = userDb.PingContext(ctx); err != nil {
		userDb.Close() // Close if ping fails
		customLog.Warnf("Storage: Failed to ping user DB '%s': %v", filePath, err)
		return nil, fmt.Errorf("failed to connect to user database storage: %w", err)
	}

	// Optional: Configure pool for user DB connections if kept open longer
	// userDB.SetMaxOpenConns(5)
	// userDB.SetMaxIdleConns(1)

	return userDb, nil
}

// --- User DB Schema Operations ---

// PragmaTableInfo retrieves schema information for a table.
func PragmaTableInfo(ctx context.Context, userDB *sql.DB, tableName string) (map[string]string, error) {
	pragmaSQL := fmt.Sprintf("PRAGMA table_info(%s);", tableName) // Assumes tableName is pre-validated
	rows, err := userDB.QueryContext(ctx, pragmaSQL)
	if err != nil {
		customLog.Warnf("Storage: Failed PRAGMA for Table '%s': %v", tableName, err)
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
			customLog.Warnf("Storage: Failed scanning PRAGMA for Table '%s': %v", tableName, err)
			return nil, fmt.Errorf("failed to parse schema: %w", err)
		}
		columnTypes[strings.ToLower(name)] = strings.ToUpper(sqlType)
	}
	if err = rows.Err(); err != nil {
		customLog.Warnf("Storage: Error iterating PRAGMA for Table '%s': %v", tableName, err)
		return nil, fmt.Errorf("failed to read schema: %w", err)
	}
	if !foundColumns {
		return nil, ErrTableNotFound // No rows means table doesn't exist
	}
	return columnTypes, nil
}

// ListTables retrieves a list of table names from the user's database file.
func ListTables(ctx context.Context, userDB *sql.DB) ([]domain.TableMetadata, error) {
	// Query sqlite_master (or sqlite_schema in newer versions) for tables
	// Exclude sqlite internal tables
	query := `SELECT * FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;`

	rows, err := userDB.QueryContext(ctx, query)

	if err != nil {
		customLog.Warnf("Storage: Error listing tables: %v", err)
		return nil, fmt.Errorf("database error listing tables: %w", err)
	}
	defer rows.Close()

	var tables []domain.TableMetadata

	for rows.Next() {
		var table domain.TableMetadata

		if err := rows.Scan(&table.Type, &table.Name, &table.TableName, &table.RootPage, &table.Sql); err != nil {
			customLog.Warnf("Storage: Error scanning table name: %v", err)
			return nil, fmt.Errorf("failed processing table list: %w", err)
		}
		// Get column information for the current table.
		columnInfos, err := getColumnInfo(ctx, userDB, table.Name)
		if err != nil {
			return nil, err // Return the error from getColumnInfo
		}
		table.Columns = columnInfos

		tables = append(tables, table)
	}
	if err = rows.Err(); err != nil {
		customLog.Warnf("Storage: Error iterating table list: %v", err)
		return nil, fmt.Errorf("failed reading table list: %w", err)
	}

	if tables == nil {
		tables = make([]domain.TableMetadata, 0)
	}
	return tables, nil
}

// CreateTable executes a CREATE TABLE statement in the user DB.
func CreateTable(ctx context.Context, userDB *sql.DB, createSQL string) error {
	_, err := userDB.ExecContext(ctx, createSQL) // createSQL assumed pre-validated
	if err != nil {
		customLog.Warnf("Storage: Failed to execute CREATE TABLE: %v\nSQL: %s", err, createSQL)
		// Could try to parse error for specific issues (e.g., table exists if not using IF NOT EXISTS)
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

// DropTable executes a DROP TABLE statement in the user DB.
// tableName should be pre-validated by the caller.
func DropTable(ctx context.Context, userDB *sql.DB, tableName string) error {
	// Use IF EXISTS to prevent error if table doesn't exist (makes operation idempotent)
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s;", tableName) // tableName is assumed validated
	_, err := userDB.ExecContext(ctx, dropSQL)

	if err != nil {
		// This could indicate a more serious issue (permissions, locked db, etc.)
		customLog.Warnf("Storage: Failed DROP TABLE for Table '%s': %v", tableName, err)
		return fmt.Errorf("database error dropping table: %w", err)
	}
	return nil
}

func ListUserTableSchema(ctx context.Context, userDB *sql.DB, tableName string) ([]domain.TableSchemaMetaData, error) {
	row := userDB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", tableName)
	var schema string

	err := row.Scan(&schema)
	if err != nil {
		return nil, err
	}

	// This splits the schema at the commas, and then attempts to extract the relevant parts.
	var columns []domain.TableSchemaMetaData
	parts := schema[strings.IndexByte(schema, '(')+1 : strings.LastIndexByte(schema, ')')]
	columnDefs := strings.SplitSeq(parts, ",")

	for colDef := range columnDefs {
		colDef = strings.TrimSpace(colDef)
		fields := strings.Fields(colDef)

		if len(fields) < 2 {
			continue // Skip invalid column definitions.
		}

		colInfo := domain.TableSchemaMetaData{
			Name: fields[0],
			Type: fields[1],
		}
		if len(fields) > 2 && fields[2] == "PRIMARY" {
			colInfo.PrimaryKey = true
		}
		columns = append(columns, colInfo)
	}

	return columns, nil
}

// --- User DB Record CRUD Operations ---

// InsertRecord executes an INSERT statement and returns the last insert ID.
func InsertRecord(ctx context.Context, userDB *sql.DB, insertSQL string, values ...interface{}) (int64, error) {
	result, err := userDB.ExecContext(ctx, insertSQL, values...)
	if err != nil {
		customLog.Warnf("Storage: Failed INSERT: %v\nSQL: %s", err, insertSQL)
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
		customLog.Warnf("Storage: Failed to get LastInsertId after INSERT: %v", err)
		return 0, fmt.Errorf("failed to retrieve ID after insert: %w", err)
	}
	return lastID, nil
}

// ListRecords retrieves records with support for filtering, pagination, sorting, and field selection.
// Accepts tableName, query parameters, and parsed query options.
func ListRecords(ctx context.Context, userDB *sql.DB, tableName string, queryParams url.Values, opts *core.ListQueryOptions) (*ListRecordsResult, error) {

	// 1. Fetch schema to validate filter keys, sort column, and field columns
	columnTypes, err := PragmaTableInfo(ctx, userDB, tableName)
	if err != nil {
		return nil, err // Propagate ErrTableNotFound or other schema errors
	}

	// 2. Validate sort column exists in schema (if specified)
	if opts.SortBy != "" {
		if _, exists := columnTypes[strings.ToLower(opts.SortBy)]; !exists {
			return nil, fmt.Errorf("%w: '%s' not found in table schema", ErrInvalidSortColumn, opts.SortBy)
		}
	}

	// 3. Validate and build field list for SELECT
	var selectFields string
	if len(opts.Fields) > 0 {
		validatedFields := make([]string, 0, len(opts.Fields))
		for _, field := range opts.Fields {
			if _, exists := columnTypes[strings.ToLower(field)]; !exists {
				return nil, fmt.Errorf("%w: '%s' not found in table schema", ErrInvalidFieldColumn, field)
			}
			validatedFields = append(validatedFields, field)
		}
		selectFields = strings.Join(validatedFields, ", ")
	} else {
		selectFields = "*"
	}

	// 4. Build WHERE clause and arguments from queryParams (excluding reserved params)
	whereClauses := []string{}
	args := []any{}

	for key, values := range queryParams {
		// Skip reserved parameters
		if core.IsReservedParam(key) {
			continue
		}

		if len(values) == 0 {
			continue
		}
		filterValueStr := values[0]
		lowerKey := strings.ToLower(key)

		// A. Validate filter key format
		if !core.IsValidIdentifier(key) {
			customLog.Warnf("Storage: ListRecords received invalid filter key format: %s", key)
			return nil, fmt.Errorf("%w: invalid filter key format '%s'", ErrInvalidFilterValue, key)
		}

		// B. Validate filter key exists in schema
		expectedType, exists := columnTypes[lowerKey]
		if !exists {
			customLog.Warnf("Storage: ListRecords received filter key not in schema: %s", key)
			return nil, fmt.Errorf("%w: filter key '%s' not found in table schema", ErrInvalidFilterValue, key)
		}

		// C. Attempt to convert filterValueStr to expected type
		var convertedValue interface{}
		var conversionError error

		switch expectedType {
		case "INTEGER", "BOOLEAN":
			if vInt, err := strconv.ParseInt(filterValueStr, 10, 64); err == nil {
				convertedValue = vInt
			} else {
				conversionError = fmt.Errorf("expected an integer for column '%s'", key)
			}
		case "REAL":
			if vFloat, err := strconv.ParseFloat(filterValueStr, 64); err == nil {
				convertedValue = vFloat
			} else {
				conversionError = fmt.Errorf("expected a number (float) for column '%s'", key)
			}
		case "TEXT":
			convertedValue = filterValueStr
		case "BLOB":
			customLog.Printf("Storage: ListRecords ignoring filter on BLOB column: %s", key)
			continue
		default:
			customLog.Printf("Storage: ListRecords ignoring filter on column '%s' with unhandled type '%s'", key, expectedType)
			continue
		}

		if conversionError != nil {
			customLog.Printf("Storage: ListRecords conversion error for key '%s', value '%s': %v", key, filterValueStr, conversionError)
			return nil, fmt.Errorf("%w: %s", ErrInvalidFilterValue, conversionError.Error())
		}

		whereClauses = append(whereClauses, fmt.Sprintf("%s = ?", key))
		args = append(args, convertedValue)
	}

	// 5. Build WHERE clause string
	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// 6. Get total count for pagination metadata
	// nolint:gosec // tableName is validated by handler before reaching here
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", tableName, whereClause)
	var totalCount int
	err = userDB.QueryRowContext(ctx, countSQL, args...).Scan(&totalCount)
	if err != nil {
		customLog.Warnf("Storage: Failed COUNT query: %v\nSQL: %s", err, countSQL)
		return nil, fmt.Errorf("database error counting records: %w", err)
	}

	// 7. Construct final SELECT SQL with ORDER BY and LIMIT/OFFSET
	// nolint:gosec // tableName and selectFields are validated
	selectSQL := fmt.Sprintf("SELECT %s FROM %s%s", selectFields, tableName, whereClause)

	// Add ORDER BY clause
	if opts.SortBy != "" {
		orderDirection := "ASC"
		if strings.ToLower(opts.SortOrder) == "desc" {
			orderDirection = "DESC"
		}
		selectSQL += fmt.Sprintf(" ORDER BY %s %s", opts.SortBy, orderDirection)
	} else {
		// Default sort by id if exists, otherwise no default sort
		if _, hasID := columnTypes["id"]; hasID {
			selectSQL += " ORDER BY id ASC"
		}
	}

	// Add LIMIT and OFFSET
	selectSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", opts.Limit, opts.Offset)

	customLog.Printf("Storage: Executing List Records SQL: %s | Args: %v", selectSQL, args)

	// 8. Execute query
	rows, err := userDB.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		customLog.Warnf("Storage: Failed SELECT: %v\nSQL: %s", err, selectSQL)
		return nil, fmt.Errorf("database error listing records: %w", err)
	}
	defer rows.Close()

	// 9. Process results
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed processing results: %w", err)
	}
	numColumns := len(columns)
	records := make([]map[string]interface{}, 0)

	for rows.Next() {
		scanArgs := make([]interface{}, numColumns)
		values := make([]interface{}, numColumns)
		for i := range values {
			scanArgs[i] = &values[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
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
		records = append(records, rowData)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed processing all records: %w", err)
	}

	return &ListRecordsResult{
		Records: records,
		Pagination: PaginationMeta{
			Total:  totalCount,
			Limit:  opts.Limit,
			Offset: opts.Offset,
		},
	}, nil
}

// GetRecord executes SELECT * WHERE id = ? and returns a single map or ErrRecordNotFound.
func GetRecord(ctx context.Context, userDB *sql.DB, selectSQL string, recordID int64) (map[string]interface{}, error) {
	rows, err := userDB.QueryContext(ctx, selectSQL, recordID) // selectSQL assumed safe with placeholder
	if err != nil {
		customLog.Warnf("Storage: Failed SELECT by ID: %v\nSQL: %s", err, selectSQL)
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
		customLog.Warnf("Storage: Failed scanning row for SELECT by ID: %v", err)
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
	if rows.Next() {
		customLog.Warnf("WARN: Found multiple rows for ID %d", recordID)
	}

	return rowData, nil
}

// UpdateRecord executes an UPDATE statement and returns rows affected.
func UpdateRecord(ctx context.Context, userDB *sql.DB, updateSQL string, values ...interface{}) (int64, error) {
	result, err := userDB.ExecContext(ctx, updateSQL, values...)
	if err != nil {
		customLog.Warnf("Storage: Failed UPDATE: %v\nSQL: %s", err, updateSQL)
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
		customLog.Warnf("Storage: Failed getting RowsAffected after UPDATE: %v", err)
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
		customLog.Warnf("Storage: Failed DELETE: %v\nSQL: %s", err, deleteSQL)
		// Less likely to get specific errors here, maybe just connection issues
		return 0, fmt.Errorf("database error during delete: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		customLog.Warnf("Storage: Failed getting RowsAffected after DELETE: %v", err)
		return 0, fmt.Errorf("failed confirming delete: %w", err)
	}
	if rowsAffected == 0 {
		return 0, ErrRecordNotFound // No rows matched the WHERE clause
	}
	return rowsAffected, nil
}

// helper function to get column information
func getColumnInfo(ctx context.Context, userDb *sql.DB, tableName string) ([]domain.ColumnInfo, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", tableName)
	rows, err := userDb.QueryContext(ctx, query)
	if err != nil {
		customLog.Warnf("Storage: Error getting column info for table %s: %v", tableName, err)
		return nil, fmt.Errorf("database error getting column info: %w", err)
	}
	defer rows.Close()

	var columnInfos []domain.ColumnInfo
	for rows.Next() {
		var colInfo domain.ColumnInfo
		if err := rows.Scan(&colInfo.ColumnId, &colInfo.Name, &colInfo.Type, &colInfo.NotNull, &colInfo.Default, &colInfo.PK); err != nil {
			customLog.Warnf("Storage: Error scanning column info: %v", err)
			return nil, fmt.Errorf("failed processing column info: %w", err)
		}
		columnInfos = append(columnInfos, colInfo)
	}
	if err = rows.Err(); err != nil {
		customLog.Warnf("Storage: Error iterating column info: %v", err)
		return nil, fmt.Errorf("failed reading column info: %w", err)
	}

	return columnInfos, nil
}
