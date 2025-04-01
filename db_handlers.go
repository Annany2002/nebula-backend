// db_handlers.go
package main

import (
	"database/sql" // Import database/sql
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// --- *** NEW: Handler for Schema Creation *** ---

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

// --- *** END NEW *** ---
