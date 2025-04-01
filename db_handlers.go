// db_handlers.go
package main

import (
	"database/sql" // Import database/sql
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings" // Import strings

	"github.com/gin-gonic/gin"
	"github.com/mattn/go-sqlite3"
)

// Regular expression for valid database/table/column names (alphanumeric + underscore)
var nameValidationRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Allowed SQLite column types for user definition (case-insensitive check, stored uppercase)
var allowedColumnTypes = map[string]string{
	"TEXT":    "TEXT",
	"INTEGER": "INTEGER",
	"REAL":    "REAL", // For floating-point numbers
	"BLOB":    "BLOB", // For binary data
	// Could simulate BOOLEAN with INTEGER (0 or 1) if needed
}

// isValidType checks if a string is an allowed column type
func isValidType(colType string) (string, bool) {
	upperType := strings.ToUpper(colType)
	normalizedType, ok := allowedColumnTypes[upperType]
	return normalizedType, ok
}

// isValidName checks if a string is a valid identifier (e.g., db_name, table_name)
func isValidName(name string) bool {
	return nameValidationRegex.MatchString(name) && len(name) > 0 && len(name) <= 64 // Added length check
}

// createDatabaseHandler handles requests to register a new user database.
func createDatabaseHandler(c *gin.Context) {
	// 1. Get UserID from context (set by AuthMiddleware)
	userID := c.MustGet("userID").(int64)

	// 2. Bind JSON request body
	var req CreateDatabaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Create DB binding error for UserID %d: %v", userID, err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 3. Validate database name
	if !isValidName(req.DBName) {
		log.Printf("Invalid DB name attempt by UserID %d: %s", userID, req.DBName)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database name. Use only alphanumeric characters and underscores (a-z, A-Z, 0-9, _), max length 64."})
		return
	}

	// 4. Construct the file path for the user's database
	// Example: data/123/my_cool_app_db.db
	userDbDir := filepath.Join(metadataDbDir, fmt.Sprintf("%d", userID))
	dbFilePath := filepath.Join(userDbDir, req.DBName+".db")

	// 5. Ensure the user-specific directory exists (e.g., data/123/)
	if err := os.MkdirAll(userDbDir, 0750); err != nil {
		log.Printf("Error creating user DB directory '%s' for UserID %d: %v", userDbDir, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to create database storage location"})
		return
	}

	// 6. Insert the database record into metadata.db
	sqlStatement := `INSERT INTO databases (user_id, db_name, file_path) VALUES (?, ?, ?)`
	_, err := metaDB.ExecContext(c.Request.Context(), sqlStatement, userID, req.DBName, dbFilePath)

	if err != nil {
		// Check for unique constraint violation (user_id, db_name)
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			// Could be the UNIQUE(user_id, db_name) or UNIQUE(file_path) constraint.
			// In our logic, file_path uniqueness implies (user_id, db_name) uniqueness.
			log.Printf("Create DB conflict for UserID %d, DBName '%s': %v", userID, req.DBName, err)
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "A database with this name already exists for your account."})
			return
		}

		// Handle other potential database errors
		log.Printf("Failed to insert database record for UserID %d, DBName '%s': %v", userID, req.DBName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to register database"})
		return
	}

	log.Printf("Successfully registered database '%s' for UserID %d at path '%s'", req.DBName, userID, dbFilePath)

	// 7. Return success response
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Database registered successfully",
		"db_name":   req.DBName,
		"file_path": dbFilePath, // Optional: maybe don't expose exact path? For MVP it's ok.
	})
}

// createSchemaHandler handles requests to define a table schema within a user's database
func createSchemaHandler(c *gin.Context) {
	// 1. Get UserID and DBName
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name") // Get db_name from URL path parameter

	// Validate dbName from URL param as well
	if !isValidName(dbName) {
		log.Printf("Invalid DB name in path for UserID %d: %s", userID, dbName)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database name in URL path."})
		return
	}

	// 2. Look up the database file path in metadata.db
	var dbFilePath string
	lookupSQL := `SELECT file_path FROM databases WHERE user_id = ? AND db_name = ? LIMIT 1`
	err := metaDB.QueryRowContext(c.Request.Context(), lookupSQL, userID, dbName).Scan(&dbFilePath)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("Schema creation attempt failed for UserID %d, DBName '%s': database not found/registered", userID, dbName)
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered for your account."})
			return
		}
		log.Printf("Error looking up database path for UserID %d, DBName '%s': %v", userID, dbName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		return
	}

	// 3. Bind the JSON schema definition request
	var req CreateSchemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Schema creation binding error for UserID %d, DBName '%s': %v", userID, dbName, err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 4. Validate Table Name and Column Definitions
	if !isValidName(req.TableName) {
		log.Printf("Invalid table name attempt by UserID %d, DBName '%s': %s", userID, dbName, req.TableName)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid table name. Use only alphanumeric characters and underscores (a-z, A-Z, 0-9, _), max length 64."})
		return
	}

	if len(req.Columns) == 0 { // Should be caught by binding:"min=1" but double check
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Schema must contain at least one column."})
		return
	}

	var columnDefs []string // To build "col_name TYPE" parts for SQL
	for _, col := range req.Columns {
		if !isValidName(col.Name) || strings.ToLower(col.Name) == "id" { // Disallow defining 'id' explicitly
			log.Printf("Invalid column name attempt by UserID %d, DBName '%s', Table '%s': %s", userID, dbName, req.TableName, col.Name)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid column name '%s'. Use valid identifiers, cannot be 'id'.", col.Name)})
			return
		}
		normalizedType, ok := isValidType(col.Type)
		if !ok {
			log.Printf("Invalid column type attempt by UserID %d, DBName '%s', Table '%s', Column '%s': %s", userID, dbName, req.TableName, col.Name, col.Type)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid type '%s' for column '%s'. Allowed types: TEXT, INTEGER, REAL, BLOB.", col.Type, col.Name)})
			return
		}
		// Build definition string safely *after* validation
		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", col.Name, normalizedType))
	}

	// 5. Connect to the specific user's database file
	log.Printf("Attempting to open user DB for schema creation: %s", dbFilePath)
	userDB, err := sql.Open("sqlite3", dbFilePath+"?_foreign_keys=on") // Enable FKs for user DBs too
	if err != nil {
		log.Printf("Failed to open user DB file '%s' for UserID %d: %v", dbFilePath, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close() // Ensure connection is closed

	// Optional: Ping to verify connection immediately (Open doesn't always connect right away)
	if err = userDB.PingContext(c.Request.Context()); err != nil {
		log.Printf("Failed to ping user DB '%s' for UserID %d: %v", dbFilePath, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to database storage."})
		return
	}

	// 6. Construct the CREATE TABLE SQL statement safely
	// Use validated table name and column definitions
	// Add implicit 'id' primary key
	// Use "IF NOT EXISTS" to make endpoint somewhat idempotent regarding table creation
	createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY AUTOINCREMENT, %s);",
		req.TableName,                  // Already validated
		strings.Join(columnDefs, ", "), // Already validated parts
	)
	log.Printf("Executing Schema SQL for UserID %d, DB '%s': %s", userID, dbName, createTableSQL)

	// 7. Execute the CREATE TABLE statement
	_, err = userDB.ExecContext(c.Request.Context(), createTableSQL)
	if err != nil {
		// Handle potential errors during table creation (e.g., syntax issues, disk errors)
		log.Printf("Failed to execute CREATE TABLE statement for UserID %d, DB '%s', Table '%s': %v", userID, dbName, req.TableName, err)
		// Check for specific SQLite errors if needed, otherwise generic 500
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to create table in database."})
		return
	}

	log.Printf("Successfully ensured table '%s' in DB '%s' for UserID %d", req.TableName, dbName, userID)

	// 8. Return success response
	c.JSON(http.StatusCreated, gin.H{ // Use 201 Created as we are defining a resource (the table)
		"message":    fmt.Sprintf("Table '%s' created or already exists.", req.TableName),
		"db_name":    dbName,
		"table_name": req.TableName,
	})
}

// createRecordHandler handles inserting a new record into a user-defined table
func createRecordHandler(c *gin.Context) {
	// 1. Get UserID, DBName, TableName
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name")
	tableName := c.Param("table_name")

	if !isValidName(dbName) || !isValidName(tableName) {
		log.Printf("Invalid DB/Table name in path for UserID %d: DB '%s', Table '%s'", userID, dbName, tableName)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database or table name in URL path."})
		return
	}

	// 2. Look up the database file path
	var dbFilePath string
	lookupSQL := `SELECT file_path FROM databases WHERE user_id = ? AND db_name = ? LIMIT 1`
	err := metaDB.QueryRowContext(c.Request.Context(), lookupSQL, userID, dbName).Scan(&dbFilePath)
	if err != nil { // Handle DB lookup errors (404, 500)
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("Record creation attempt failed for UserID %d, DB '%s': database not found/registered", userID, dbName)
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered for your account."})
			return
		}
		log.Printf("Error looking up database path for UserID %d, DB '%s': %v", userID, dbName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		return
	}

	// 3. Connect to the specific user's database file
	userDB, err := sql.Open("sqlite3", dbFilePath+"?_foreign_keys=on")
	if err != nil { // Handle DB connection errors (500)
		log.Printf("Failed to open user DB file '%s' for UserID %d: %v", dbFilePath, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()
	if err = userDB.PingContext(c.Request.Context()); err != nil {
		log.Printf("Failed to ping user DB '%s' for UserID %d: %v", dbFilePath, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to database storage."})
		return
	}

	// *** NEW: 4. Fetch Table Schema using PRAGMA ***
	pragmaSQL := fmt.Sprintf("PRAGMA table_info(%s);", tableName) // Safe as tableName is validated
	rows, err := userDB.QueryContext(c.Request.Context(), pragmaSQL)
	if err != nil {
		log.Printf("Failed to execute PRAGMA table_info for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		// This might indicate the table doesn't exist, treat as 404 or 500
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve table schema."})
		return
	}
	defer rows.Close()

	columnTypes := make(map[string]string) // Map column name -> uppercase SQL type
	foundColumns := false
	for rows.Next() {
		foundColumns = true
		var cid int                  // Column ID
		var name string              // Column name
		var sqlType string           // Column type (e.g., "INTEGER", "TEXT")
		var notnull int              // Non-null constraint (0 or 1)
		var dfltValue sql.NullString // Default value
		var pk int                   // Primary key flag (0 or 1)

		if err := rows.Scan(&cid, &name, &sqlType, &notnull, &dfltValue, &pk); err != nil {
			log.Printf("Failed to scan PRAGMA table_info row for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table schema."})
			return
		}
		// Store type in uppercase for consistent checks
		columnTypes[strings.ToLower(name)] = strings.ToUpper(sqlType) // Use lowercase key for map lookup consistency
	}
	if err = rows.Err(); err != nil { // Check for errors during iteration
		log.Printf("Error iterating PRAGMA table_info results for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to read table schema."})
		return
	}
	if !foundColumns {
		// PRAGMA returned no rows, likely means table doesn't exist
		log.Printf("PRAGMA table_info returned no results for UserID %d, DB '%s', Table '%s'. Assuming table not found.", userID, dbName, tableName)
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
		return
	}

	// 5. Bind arbitrary JSON request body
	var recordData map[string]interface{}
	if err := c.ShouldBindJSON(&recordData); err != nil { // Handle binding errors (400)
		log.Printf("Record creation binding error for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON request body: " + err.Error()})
		return
	}
	if len(recordData) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Request body cannot be empty."})
		return
	}

	// 6. Prepare for dynamic SQL construction & Validate Input Data Types
	var columns []string
	var placeholders []string
	var values []interface{}

	for key, val := range recordData {
		lowerKey := strings.ToLower(key) // Use lowercase for lookup consistency

		// A. Validate column name is valid format and not 'id'
		if !isValidName(key) || lowerKey == "id" {
			log.Printf("Record creation: Ignored invalid/disallowed key '%s' for UserID %d, DB '%s', Table '%s'", key, userID, dbName, tableName)
			continue // Skip this key-value pair
		}

		// B. Check if column exists in the schema and get expected type
		expectedType, exists := columnTypes[lowerKey]
		if !exists {
			log.Printf("Record creation: Column '%s' not found in table '%s' schema for UserID %d.", key, tableName, userID)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Column '%s' does not exist in table '%s'.", key, tableName)})
			return
		}

		// C. *** NEW: Validate Go type against expected SQL type ***
		isValidValue := false
		switch expectedType {
		case "INTEGER":
			// JSON numbers are float64 by default. Check if it's a whole number.
			if vFloat, ok := val.(float64); ok {
				if math.Floor(vFloat) == vFloat {
					isValidValue = true
					// Optional: Convert to int64 for insertion if preferred, though driver handles float64 for INTEGER
					// values = append(values, int64(vFloat))
					// continue // Skip default append below if converting type
				}
			} else if _, ok := val.(int); ok { // Handle if JSON lib somehow gives int
				isValidValue = true
			} else if _, ok := val.(int64); ok { // Handle if JSON lib somehow gives int64
				isValidValue = true
			}
		case "REAL":
			// Accept float64 or potentially int/int64 from JSON
			if _, ok := val.(float64); ok {
				isValidValue = true
			} else if _, ok := val.(int); ok {
				isValidValue = true
			} else if _, ok := val.(int64); ok {
				isValidValue = true
			}
		case "TEXT":
			if _, ok := val.(string); ok {
				isValidValue = true
			}
		case "BLOB":
			// For MVP, we might just accept strings (assuming base64) or nil
			// Or skip validation and let SQLite handle it. For now, let's be lenient.
			// If strictness is needed, check for string and maybe try base64 decoding.
			isValidValue = true // Lenient for now
		case "BOOLEAN": // If we simulated BOOLEAN as INTEGER
			if _, ok := val.(bool); ok {
				isValidValue = true // Driver usually handles bool to 0/1
			} else if vFloat, ok := val.(float64); ok && (vFloat == 0 || vFloat == 1) {
				isValidValue = true // Allow 0 or 1 from JSON numbers
			}

		default:
			// Should not happen if PRAGMA worked correctly
			log.Printf("WARNING: Unknown SQL type '%s' encountered for column '%s' during type validation.", expectedType, key)
			isValidValue = true // Be lenient with unknown types? Or return error?
		}

		if !isValidValue {
			log.Printf("Record creation: Type mismatch for column '%s' (expected %s) for UserID %d. Received type %T.", key, expectedType, userID, val)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid data type for column '%s'. Expected %s.", key, expectedType)})
			return
		}

		// D. If all validations pass, add to lists for SQL construction
		columns = append(columns, key) // Use original key casing for SQL clarity if desired, or lowerKey
		placeholders = append(placeholders, "?")
		values = append(values, val)
	} // End loop over recordData

	if len(columns) == 0 { // Check if any valid columns were actually provided
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "No valid or recognized columns provided in request body."})
		return
	}

	// 7. Construct the dynamic INSERT SQL statement
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,                        // Validated
		strings.Join(columns, ", "),      // Built from validated & existing keys
		strings.Join(placeholders, ", "), // Placeholders match `values` slice
	)
	log.Printf("Executing Record Insert SQL for UserID %d, DB '%s', Table '%s': %s", userID, dbName, tableName, insertSQL)

	// 8. Execute the INSERT statement
	result, err := userDB.ExecContext(c.Request.Context(), insertSQL, values...)
	if err != nil { // Handle execution errors (500, 409, etc.)
		log.Printf("Failed to execute INSERT statement for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		// Simplified error handling, as pre-validation should catch many issues
		errMsg := "Failed to insert record."
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			errMsg = "Database constraint violation (e.g., UNIQUE constraint failed)."
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": errMsg})
			return
		}
		// Add back specific checks if needed, but rely more on pre-validation now
		// if strings.Contains(err.Error(), "datatype mismatch") { ... } // Less likely now

		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	// 9. Get the ID of the newly inserted record
	lastID, err := result.LastInsertId()
	if err != nil { // Handle failure to get ID (500 or partial success)
		log.Printf("Failed to get LastInsertId for UserID %d, DB '%s', Table '%s' (but insert likely succeeded): %v", userID, dbName, tableName, err)
		c.JSON(http.StatusCreated, gin.H{"message": "Record created successfully (failed to retrieve ID)."})
		return
	}

	log.Printf("Successfully inserted record with ID %d into DB '%s', Table '%s' for UserID %d", lastID, dbName, tableName, userID)

	// 10. Return success response
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Record created successfully",
		"record_id": lastID,
	})
}

// listRecordsHandler handles retrieving all records from a user-defined table
func listRecordsHandler(c *gin.Context) {
	// 1. Get UserID, DBName, TableName
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name")
	tableName := c.Param("table_name")

	if !isValidName(dbName) || !isValidName(tableName) {
		log.Printf("List Records: Invalid DB/Table name in path for UserID %d: DB '%s', Table '%s'", userID, dbName, tableName)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database or table name in URL path."})
		return
	}

	// 2. Look up the database file path
	var dbFilePath string
	lookupSQL := `SELECT file_path FROM databases WHERE user_id = ? AND db_name = ? LIMIT 1`
	err := metaDB.QueryRowContext(c.Request.Context(), lookupSQL, userID, dbName).Scan(&dbFilePath)
	if err != nil { // Handle DB lookup errors (404, 500)
		if errors.Is(err, sql.ErrNoRows) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		return
	}

	// 3. Connect to the specific user's database file
	userDB, err := sql.Open("sqlite3", dbFilePath+"?_foreign_keys=on")
	if err != nil { // Handle DB connection errors (500)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()
	if err = userDB.PingContext(c.Request.Context()); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to database storage."})
		return
	}

	// 4. Construct the SELECT * SQL statement
	// Using SELECT * is simple for MVP. In production, specifying columns is better.
	selectSQL := fmt.Sprintf("SELECT * FROM %s;", tableName) // tableName is validated
	log.Printf("Executing Record List SQL for UserID %d, DB '%s', Table '%s': %s", userID, dbName, tableName, selectSQL)

	// 5. Execute the SELECT query
	rows, err := userDB.QueryContext(c.Request.Context(), selectSQL)
	if err != nil {
		// Handle potential errors: table not found most likely
		log.Printf("Failed to execute SELECT query for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		if strings.Contains(err.Error(), "no such table") {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to query records."})
		return
	}
	defer rows.Close()

	// 6. Process the results into a slice of maps
	columns, err := rows.Columns()
	if err != nil {
		log.Printf("Failed to get columns for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query results."})
		return
	}
	numColumns := len(columns)
	results := make([]map[string]interface{}, 0) // Initialize empty slice

	for rows.Next() {
		scanArgs := make([]interface{}, numColumns) // Slice of pointers
		values := make([]interface{}, numColumns)   // Slice to hold actual values

		// Point scanArgs pointers to the elements of the values slice
		for i := range values {
			scanArgs[i] = &values[i]
		}

		// Scan the row into the pointers
		if err := rows.Scan(scanArgs...); err != nil {
			log.Printf("Failed to scan row for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to read record data."})
			return
		}

		// Create a map for the current row
		rowData := make(map[string]interface{})
		for i, colName := range columns {
			// Handle potential []byte values (common for TEXT, BLOB)
			// and convert them to string for JSON compatibility
			rawValue := values[i]
			if byteSlice, ok := rawValue.([]byte); ok {
				rowData[colName] = string(byteSlice) // Convert []byte to string
			} else {
				rowData[colName] = rawValue // Use the value directly for other types (int64, float64, nil)
			}
		}
		results = append(results, rowData)
	} // End rows.Next() loop

	// Check for errors that occurred during iteration
	if err = rows.Err(); err != nil {
		log.Printf("Error during row iteration for UserID %d, DB '%s', Table '%s': %v", userID, dbName, tableName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to process all records."})
		return
	}

	log.Printf("Successfully retrieved %d records from DB '%s', Table '%s' for UserID %d", len(results), dbName, tableName, userID)

	// 7. Return the results
	c.JSON(http.StatusOK, results) // Return 200 OK with the slice of records (can be empty)
}

// getRecordHandler handles retrieving a single record by ID
func getRecordHandler(c *gin.Context) {
	// 1. Get UserID, DBName, TableName, RecordID
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name")
	tableName := c.Param("table_name")
	recordIDStr := c.Param("record_id")

	if !isValidName(dbName) || !isValidName(tableName) {
		log.Printf("Get Record: Invalid DB/Table name in path for UserID %d: DB '%s', Table '%s'", userID, dbName, tableName)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database or table name in URL path."})
		return
	}

	// Validate and convert record_id
	recordID, err := strconv.ParseInt(recordIDStr, 10, 64)
	if err != nil {
		log.Printf("Get Record: Invalid Record ID format for UserID %d: %s", userID, recordIDStr)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid record ID format. Must be an integer."})
		return
	}

	// 2. Look up the database file path
	var dbFilePath string
	lookupSQL := `SELECT file_path FROM databases WHERE user_id = ? AND db_name = ? LIMIT 1`
	err = metaDB.QueryRowContext(c.Request.Context(), lookupSQL, userID, dbName).Scan(&dbFilePath)
	if err != nil { // Handle DB lookup errors (404, 500)
		if errors.Is(err, sql.ErrNoRows) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		return
	}

	// 3. Connect to the specific user's database file
	userDB, err := sql.Open("sqlite3", dbFilePath+"?_foreign_keys=on")
	if err != nil { // Handle DB connection errors (500)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()
	if err = userDB.PingContext(c.Request.Context()); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to database storage."})
		return
	}

	// 4. Construct the SELECT * ... WHERE id = ? SQL statement
	selectSQL := fmt.Sprintf("SELECT * FROM %s WHERE id = ? LIMIT 1;", tableName) // tableName validated
	log.Printf("Executing Record Get SQL for UserID %d, DB '%s', Table '%s', ID %d: %s", userID, dbName, tableName, recordID, selectSQL)

	// 5. Execute the SELECT query for a single row
	// row := userDB.QueryRowContext(c.Request.Context(), selectSQL, recordID)

	// 6. Process the single result
	// We need columns first to scan correctly into map
	// This is slightly inefficient as QueryRow doesn't expose columns directly before Scan.
	// Alternative: Use QueryContext, get columns, then check if Next() is true only once.
	// Let's stick to QueryRow for simplicity, but fetch columns with a dummy query first (or assume they are consistent)
	// Better approach: Use QueryContext instead of QueryRowContext
	rows, err := userDB.QueryContext(c.Request.Context(), selectSQL, recordID)
	if err != nil {
		log.Printf("Failed to execute SELECT query for single record UserID %d, DB '%s', Table '%s', ID %d: %v", userID, dbName, tableName, recordID, err)
		if strings.Contains(err.Error(), "no such table") {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to query record."})
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		log.Printf("Failed to get columns for single record UserID %d, DB '%s', Table '%s', ID %d: %v", userID, dbName, tableName, recordID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query results."})
		return
	}
	numColumns := len(columns)

	// Check if there is a row
	if !rows.Next() {
		// Check for error first, maybe connection died
		if err = rows.Err(); err != nil {
			log.Printf("Error checking row existence for UserID %d, DB '%s', Table '%s', ID %d: %v", userID, dbName, tableName, recordID, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for record."})
			return
		}
		// No error, just no rows found
		log.Printf("Record not found for UserID %d, DB '%s', Table '%s', ID %d", userID, dbName, tableName, recordID)
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Record not found."})
		return
	}

	// Prepare to scan the single row found
	scanArgs := make([]interface{}, numColumns)
	values := make([]interface{}, numColumns)
	for i := range values {
		scanArgs[i] = &values[i]
	}

	// Scan the row
	if err := rows.Scan(scanArgs...); err != nil {
		// This specific check handles the case where QueryRowContext is used instead of QueryContext
		// if errors.Is(err, sql.ErrNoRows) {
		// 	log.Printf("Record not found for UserID %d, DB '%s', Table '%s', ID %d", userID, dbName, tableName, recordID)
		// 	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Record not found."})
		// 	return
		// }
		// Handle other potential scan errors
		log.Printf("Failed to scan row for single record UserID %d, DB '%s', Table '%s', ID %d: %v", userID, dbName, tableName, recordID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to read record data."})
		return
	}

	// Ensure no more rows (shouldn't happen with LIMIT 1 and primary key lookup)
	if rows.Next() {
		log.Printf("WARN: More than one row found for UserID %d, DB '%s', Table '%s', ID %d", userID, dbName, tableName, recordID)
		// Still return the first one found, but log warning
	}

	// Create a map for the row data
	rowData := make(map[string]interface{})
	for i, colName := range columns {
		rawValue := values[i]
		if byteSlice, ok := rawValue.([]byte); ok {
			rowData[colName] = string(byteSlice)
		} else {
			rowData[colName] = rawValue
		}
	}

	log.Printf("Successfully retrieved record ID %d from DB '%s', Table '%s' for UserID %d", recordID, dbName, tableName, userID)

	// 7. Return the single record
	c.JSON(http.StatusOK, rowData)
}

// --- *** NEW: Handler for Updating Records *** ---

// updateRecordHandler handles updating fields of an existing record

func updateRecordHandler(c *gin.Context) {
	// 1. Get UserID, DBName, TableName, RecordID
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name")
	tableName := c.Param("table_name")
	recordIDStr := c.Param("record_id")

	if !isValidName(dbName) || !isValidName(tableName) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database or table name in URL path."})
		return
	}
	recordID, err := strconv.ParseInt(recordIDStr, 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid record ID format. Must be an integer."})
		return
	}

	// 2. Look up the database file path
	var dbFilePath string
	lookupSQL := `SELECT file_path FROM databases WHERE user_id = ? AND db_name = ? LIMIT 1`
	err = metaDB.QueryRowContext(c.Request.Context(), lookupSQL, userID, dbName).Scan(&dbFilePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
			return
		}
		log.Printf("Update Record: Error looking up db path for UserID %d, DB %s: %v", userID, dbName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		return
	}

	// 3. Connect to the specific user's database file
	userDB, err := sql.Open("sqlite3", dbFilePath+"?_foreign_keys=on")
	if err != nil {
		log.Printf("Update Record: Failed to open user DB %s for UserID %d: %v", dbFilePath, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()
	if err = userDB.PingContext(c.Request.Context()); err != nil {
		log.Printf("Update Record: Failed to ping user DB %s for UserID %d: %v", dbFilePath, userID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to database storage."})
		return
	}

	// 4. Fetch Table Schema for validation
	pragmaSQL := fmt.Sprintf("PRAGMA table_info(%s);", tableName) // Safe as tableName is validated
	rows, err := userDB.QueryContext(c.Request.Context(), pragmaSQL)
	if err != nil {
		log.Printf("Update Record: Failed PRAGMA for UserID %d, DB %s, Table %s: %v", userID, dbName, tableName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve table schema."})
		return
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
			log.Printf("Update Record: Failed scanning PRAGMA for UserID %d, DB %s, Table %s: %v", userID, dbName, tableName, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse table schema."})
			return
		}
		columnTypes[strings.ToLower(name)] = strings.ToUpper(sqlType)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Update Record: Error iterating PRAGMA for UserID %d, DB %s, Table %s: %v", userID, dbName, tableName, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to read table schema."})
		return
	}
	if !foundColumns {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
		return
	}

	// 5. Bind JSON request body containing fields to update
	var updateData map[string]interface{}
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON request body: " + err.Error()})
		return
	}
	if len(updateData) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Request body cannot be empty for update."})
		return
	}

	// 6. Prepare for dynamic UPDATE SQL & Validate Input Data Types
	var setClauses []string
	var values []interface{}

	for key, val := range updateData {
		lowerKey := strings.ToLower(key)

		// A. Validate column name is valid format and not 'id'
		if !isValidName(key) || lowerKey == "id" {
			log.Printf("Update Record: Ignored invalid/disallowed key '%s' for UserID %d, ID %d", key, userID, recordID)
			continue // Skip invalid/disallowed keys
		}

		// B. Check if column exists in the schema
		expectedType, exists := columnTypes[lowerKey]
		if !exists {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Column '%s' does not exist in table '%s'.", key, tableName)})
			return
		}

		// C. CORRECTED: Validate Go type against expected SQL type using type switch
		isValidValue := false
		switch expectedType {
		case "INTEGER":
			switch v := val.(type) {
			case float64:
				if math.Floor(v) == v {
					isValidValue = true
				} // Check if float represents a whole number
			case int, int64: // Accept int or int64 directly
				isValidValue = true
			case nil: // Allow null values
				isValidValue = true
			}
		case "REAL":
			switch val.(type) {
			case float64, int, int64: // Accept float or int types
				isValidValue = true
			case nil:
				isValidValue = true
			}
		case "TEXT":
			switch val.(type) {
			case string:
				isValidValue = true
			case nil:
				isValidValue = true
			}
		case "BLOB":
			// Lenient: accept string (for base64 maybe) or nil
			switch val.(type) {
			case string, nil:
				isValidValue = true
				// Add case []byte if needed, though JSON binding won't produce this directly
			}
		case "BOOLEAN": // If simulated
			switch v := val.(type) {
			case bool:
				isValidValue = true
			case float64: // Handles 0.0 or 1.0 from JSON
				if v == 0 || v == 1 {
					isValidValue = true
				}
			case nil:
				isValidValue = true
			}
		default:
			log.Printf("Update Record WARNING: Unknown expected SQL type '%s' for column '%s'. Allowing value.", expectedType, key)
			isValidValue = true // Be lenient with unknown types from PRAGMA
		}

		if !isValidValue {
			// Log the received value and type for better debugging
			log.Printf("Update Record: Type mismatch for column '%s' (expected compatible with %s) for UserID %d. Received type %T, value %v.", key, expectedType, userID, val, val)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid data type for column '%s'. Expected compatible with %s.", key, expectedType)})
			return
		}

		// D. If valid, add to lists for SQL construction
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", key)) // Using original key name for the SET clause
		values = append(values, val)
	} // End loop

	if len(setClauses) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "No valid fields provided for update."})
		return
	}

	// IMPORTANT: Append the recordID for the WHERE clause *after* the SET values
	values = append(values, recordID)

	// 7. Construct the dynamic UPDATE SQL statement
	updateSQL := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?",
		tableName,                      // Validated
		strings.Join(setClauses, ", "), // Built from validated fields
	)
	log.Printf("Executing Record Update SQL for UserID %d, ID %d: %s", userID, recordID, updateSQL)

	// 8. Execute the UPDATE statement
	result, err := userDB.ExecContext(c.Request.Context(), updateSQL, values...)
	if err != nil { // Handle execution errors (500, 409)
		log.Printf("Failed to execute UPDATE statement for UserID %d, ID %d: %v", userID, recordID, err)
		errMsg := "Failed to update record."
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			errMsg = "Database constraint violation during update (e.g., UNIQUE constraint failed)."
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": errMsg})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	// 9. Check if any rows were actually affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Failed to get RowsAffected for update UserID %d, ID %d: %v", userID, recordID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to confirm update status."})
		return
	}
	if rowsAffected == 0 {
		log.Printf("Update attempted, but record not found for UserID %d, ID %d", userID, recordID)
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Record not found for update."})
		return
	}

	log.Printf("Successfully updated record ID %d in DB '%s', Table '%s' for UserID %d", recordID, dbName, tableName, userID)

	// 10. Return success response
	c.JSON(http.StatusOK, gin.H{
		"message":       "Record updated successfully",
		"record_id":     recordID,
		"rows_affected": rowsAffected,
	})
}

// --- *** END NEW *** ---
