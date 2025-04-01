// db_handlers.go
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp" // Import regexp for validation

	"github.com/gin-gonic/gin"
	"github.com/mattn/go-sqlite3"
)

// Regular expression for valid database/table/column names (alphanumeric + underscore)
var nameValidationRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

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
