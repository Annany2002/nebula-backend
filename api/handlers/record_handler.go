// api/handlers/record_handler.go
package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	// "nebula-backend/api/models" // Not using specific models here yet
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/core"    // For validation
	"github.com/Annany2002/nebula-backend/internal/storage" // For DB operations
)

// RecordHandler holds dependencies for record CRUD handlers.
type RecordHandler struct {
	MetaDB *sql.DB        // Metadata DB pool
	Cfg    *config.Config // App configuration
	// UserRepo *storage.UserDBRepo // Could inject repo struct later
}

// NewRecordHandler creates a new RecordHandler.
func NewRecordHandler(metaDB *sql.DB, cfg *config.Config) *RecordHandler {
	return &RecordHandler{
		MetaDB: metaDB,
		Cfg:    cfg,
	}
}

// --- Helper to get User DB connection ---
// Avoids repeating lookup/connect logic in every handler
func (h *RecordHandler) getUserDBConn(c *gin.Context) (*sql.DB, string, string, error) {
	userId := c.MustGet("userId").(string)
	dbName := c.Param("db_name")
	tableName := c.Param("table_name")

	if !core.IsValidIdentifier(dbName) || !core.IsValidIdentifier(tableName) {
		return nil, "", "", errors.New("invalid database or table name in URL path") // Return error
	}

	dbFilePath, err := storage.FindDatabasePath(c.Request.Context(), h.MetaDB, userId, dbName)
	if err != nil {
		return nil, "", "", err // Return storage error (e.g., ErrDatabaseNotFound)
	}

	userDB, err := storage.ConnectUserDB(c.Request.Context(), dbFilePath)
	if err != nil {
		return nil, "", "", err // Return connection error
	}

	// Return tableName as well for convenience
	return userDB, tableName, dbFilePath, nil
}

// CreateRecord handles inserting a new record.
func (h *RecordHandler) CreateRecord(c *gin.Context) {
	userDB, tableName, dbFilePath, err := h.getUserDBConn(c)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else if strings.Contains(err.Error(), "invalid database or table name") {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		}
		return
	}
	defer userDB.Close()

	// Fetch schema for validation
	columnTypes, err := storage.PragmaTableInfo(c.Request.Context(), userDB, tableName)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrTableNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve table schema."})
		}
		return
	}

	// Bind JSON
	var recordData map[string]any
	if err := c.ShouldBindJSON(&recordData); err != nil {
		_ = c.Error(fmt.Errorf("binding error: %w", err))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON request body: " + err.Error()})
		return
	}
	if len(recordData) == 0 {
		_ = c.Error(errors.New("empty request body"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Request body cannot be empty."})
		return
	}

	// Prepare SQL parts and validate types
	var columns []string
	var placeholders []string
	var values []any

	for key, val := range recordData {
		lowerKey := strings.ToLower(key)
		if !core.IsValidIdentifier(key) || lowerKey == "id" {
			continue
		} // Skip invalid/id

		expectedType, exists := columnTypes[lowerKey]
		if !exists {
			err := fmt.Errorf("column '%s' does not exist", key)
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Perform type validation (copied logic from corrected update handler)
		isValidValue := false
		switch expectedType {
		case "INTEGER":
			switch v := val.(type) {
			case float64:
				if math.Floor(v) == v {
					isValidValue = true
				}
			case int, int64:
				isValidValue = true
			case nil:
				isValidValue = true
			}
		case "REAL":
			switch val.(type) {
			case float64, int, int64, nil:
				isValidValue = true
			}
		case "TEXT":
			switch val.(type) {
			case string, nil:
				isValidValue = true
			}
		case "BLOB":
			switch val.(type) {
			case string, nil:
				isValidValue = true
			} // Lenient
		case "BOOLEAN":
			switch v := val.(type) {
			case bool:
				isValidValue = true
			case float64:
				if v == 0 || v == 1 {
					isValidValue = true
				}
			case nil:
				isValidValue = true
			}
		default:
			isValidValue = true // Lenient
		}

		if !isValidValue {
			err := fmt.Errorf("invalid data type for column '%s'. Expected compatible with %s", key, expectedType)
			_ = c.Error(err)
			customLog.Warnf("Create Record Type Error: Key: %s, Expected: %s, Got Type: %T, Got Value: %v", key, expectedType, val, val)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		columns = append(columns, key)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	} // End validation loop

	if len(columns) == 0 {
		_ = c.Error(errors.New("no valid columns provided"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "No valid columns found in request body."})
		return
	}

	// Construct and execute INSERT via storage function
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
	customLog.Printf("Handler: Executing Create Record SQL for DB '%s': %s", dbFilePath, insertSQL)

	lastID, err := storage.InsertRecord(c.Request.Context(), userDB, insertSQL, values...)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrTableNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Table not found."})
		} else if errors.Is(err, storage.ErrColumnNotFound) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Column not found."})
		} else if errors.Is(err, storage.ErrTypeMismatch) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Data type mismatch."})
		} else if errors.Is(err, storage.ErrConstraintViolation) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "Constraint violation."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert record."})
		}
		return
	}

	customLog.Printf("Handler: Successfully inserted record ID %d into DB '%s', Table '%s'", lastID, dbFilePath, tableName)
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Record created successfully",
		"record_id": lastID,
	})
}

// ListRecords handles retrieving all records with filtering ---
func (h *RecordHandler) ListRecords(c *gin.Context) {
	userDB, tableName, dbFilePath, err := h.getUserDBConn(c)
	if err != nil {
		// Handle getUserDBConn error (400, 404, 500)
		errToSet := err // Capture error for c.Error
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else if strings.Contains(err.Error(), "invalid database or table name") {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		}
		_ = c.Error(errToSet) // Attach original error
		return
	}
	defer userDB.Close()

	// Pass the raw query parameters directly to the storage function
	queryParams := c.Request.URL.Query() // type url.Values which is map[string][]string

	customLog.Printf("Handler: Listing Records for DB '%s', Table '%s' with filters: %v", dbFilePath, tableName, queryParams)

	// Call the updated storage function
	results, err := storage.ListRecords(c.Request.Context(), userDB, tableName, queryParams)
	if err != nil {
		_ = c.Error(err) // Attach storage error
		// Map specific errors returned from storage
		if errors.Is(err, storage.ErrTableNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
		} else if errors.Is(err, storage.ErrInvalidFilterValue) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else // Handle new validation error
		{
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to query records."})
		}
		return
	}

	customLog.Printf("Handler: Successfully retrieved %d records from DB '%s', Table '%s'", len(results), dbFilePath, tableName)
	c.JSON(http.StatusOK, results)
}

// GetRecord handles retrieving a single record by ID.
func (h *RecordHandler) GetRecord(c *gin.Context) {
	recordIDStr := c.Param("record_id")
	recordID, err := strconv.ParseInt(recordIDStr, 10, 64)
	if err != nil {
		_ = c.Error(fmt.Errorf("invalid record_id format: %w", err))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid record ID format."})
		return
	}

	userDB, tableName, dbFilePath, err := h.getUserDBConn(c)
	if err != nil { /* ... handle getUserDBConn error (400, 404, 500) ... */
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else if strings.Contains(err.Error(), "invalid database or table name") {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		}
		return
	}
	defer userDB.Close()

	selectSQL := fmt.Sprintf("SELECT * FROM %s WHERE id = ? LIMIT 1;", tableName)
	customLog.Printf("Handler: Executing Get Record SQL for DB '%s', ID %d: %s", dbFilePath, recordID, selectSQL)

	recordData, err := storage.GetRecord(c.Request.Context(), userDB, selectSQL, recordID)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrTableNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
		} else if errors.Is(err, storage.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Record not found."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve record."})
		}
		return
	}

	customLog.Printf("Handler: Successfully retrieved record ID %d from DB '%s', Table '%s'", recordID, dbFilePath, tableName)
	c.JSON(http.StatusOK, recordData)
}

// UpdateRecord handles updating an existing record.
func (h *RecordHandler) UpdateRecord(c *gin.Context) {
	recordIDStr := c.Param("record_id")
	recordID, err := strconv.ParseInt(recordIDStr, 10, 64)
	if err != nil {
		_ = c.Error(fmt.Errorf("invalid record_id format: %w", err))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid record ID format."})
		return
	}

	userDB, tableName, dbFilePath, err := h.getUserDBConn(c)
	if err != nil { /* ... handle getUserDBConn error (400, 404, 500) ... */
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else if strings.Contains(err.Error(), "invalid database or table name") {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		}
		return
	}
	defer userDB.Close()

	// Fetch schema for validation
	columnTypes, err := storage.PragmaTableInfo(c.Request.Context(), userDB, tableName)
	if err != nil { /* ... handle Pragma error (404, 500) ... */
		_ = c.Error(err)
		if errors.Is(err, storage.ErrTableNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Table '%s' not found.", tableName)})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve table schema."})
		}
		return
	}

	// Bind JSON
	var updateData map[string]interface{}
	if err := c.ShouldBindJSON(&updateData); err != nil { /* ... handle binding error (400) ... */
		_ = c.Error(fmt.Errorf("binding error: %w", err))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON request body: " + err.Error()})
		return
	}
	if len(updateData) == 0 { /* ... handle empty body (400) ... */
		_ = c.Error(errors.New("empty request body for update"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Request body cannot be empty for update."})
		return
	}

	// Prepare SQL parts and validate types
	var setClauses []string
	var values []any

	for key, val := range updateData {
		lowerKey := strings.ToLower(key)
		if !core.IsValidIdentifier(key) || lowerKey == "id" {
			continue
		} // Skip

		expectedType, exists := columnTypes[lowerKey]
		if !exists { /* ... handle column not exists (400) ... */
			err := fmt.Errorf("column '%s' does not exist", key)
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Type validation logic (same as create)
		isValidValue := false
		switch expectedType { /* ... same validation switch as CreateRecord ... */
		case "INTEGER":
			switch v := val.(type) {
			case float64:
				if math.Floor(v) == v {
					isValidValue = true
				}
			case int, int64, nil:
				isValidValue = true
			}
		case "REAL":
			switch val.(type) {
			case float64, int, int64, nil:
				isValidValue = true
			}
		case "TEXT":
			switch val.(type) {
			case string, nil:
				isValidValue = true
			}
		case "BLOB":
			switch val.(type) {
			case string, nil:
				isValidValue = true
			} // Lenient
		case "BOOLEAN":
			switch v := val.(type) {
			case bool, nil:
				isValidValue = true
			case float64:
				if v == 0 || v == 1 {
					isValidValue = true
				}
			}
		default:
			isValidValue = true // Lenient
		}

		if !isValidValue { /* ... handle type mismatch (400) ... */
			err := fmt.Errorf("invalid data type for column '%s'. Expected compatible with %s", key, expectedType)
			_ = c.Error(err)
			customLog.Warnf("Update Record Type Error: Key: %s, Expected: %s, Got Type: %T, Got Value: %v", key, expectedType, val, val)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", key))
		values = append(values, val)
	} // End validation loop

	if len(setClauses) == 0 { /* ... handle no valid fields (400) ... */
		_ = c.Error(errors.New("no valid fields provided for update"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "No valid fields provided for update."})
		return
	}

	values = append(values, recordID) // Add ID for WHERE clause

	// Construct and execute UPDATE via storage function
	updateSQL := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?",
		tableName, strings.Join(setClauses, ", "))
	customLog.Printf("Handler: Executing Update Record SQL for DB '%s', ID %d: %s", dbFilePath, recordID, updateSQL)

	_, err = storage.UpdateRecord(c.Request.Context(), userDB, updateSQL, values...)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrTableNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Table not found."})
		} else // Should have been caught by PRAGMA
		if errors.Is(err, storage.ErrColumnNotFound) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Column not found."})
		} else // Should have been caught by PRAGMA/validation
		if errors.Is(err, storage.ErrTypeMismatch) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Data type mismatch."})
		} else // Should have been caught by validation
		if errors.Is(err, storage.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Record not found for update."})
		} else // From RowsAffected check in repo
		if errors.Is(err, storage.ErrConstraintViolation) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "Constraint violation."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to update record."})
		}
		return
	}

	customLog.Printf("Handler: Successfully updated record ID %d in DB '%s', Table '%s'", recordID, dbFilePath, tableName)
	c.JSON(http.StatusOK, gin.H{
		"message":   "Record updated successfully",
		"record_id": recordID,
	})
}

// DeleteRecord handles deleting a specific record by ID.
func (h *RecordHandler) DeleteRecord(c *gin.Context) {
	recordIDStr := c.Param("record_id")
	recordID, err := strconv.ParseInt(recordIDStr, 10, 64)
	if err != nil { /* ... handle invalid ID (400) ... */
		_ = c.Error(fmt.Errorf("invalid record_id format: %w", err))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid record ID format."})
		return
	}

	userDB, tableName, dbFilePath, err := h.getUserDBConn(c)
	if err != nil { /* ... handle getUserDBConn error (400, 404, 500) ... */
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else if strings.Contains(err.Error(), "invalid database or table name") {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		}
		return
	}
	defer userDB.Close()

	// Construct and execute DELETE via storage function
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = ?", tableName)
	customLog.Printf("Handler: Executing Delete Record SQL for DB '%s', ID %d: %s", dbFilePath, recordID, deleteSQL)

	_, err = storage.DeleteRecord(c.Request.Context(), userDB, deleteSQL, recordID)
	if err != nil {
		_ = c.Error(err)
		// ErrTableNotFound might occur if race condition, but unlikely
		if errors.Is(err, storage.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Record not found for deletion."})
		} else // From RowsAffected check in repo
		{
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete record."})
		}
		return
	}

	customLog.Printf("Handler: Successfully deleted record ID %d from DB '%s', Table '%s'", recordID, dbFilePath, tableName)
	c.Status(http.StatusNoContent) // Use 204 No Content
}
