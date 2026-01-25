// api/handlers/table_handler.go
package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/config"
	nebulaErrors "github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/core"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// TableHandler holds dependencies for table management handlers.
type TableHandler struct {
	MetaDB *sql.DB        // Metadata DB pool (needed for path lookup)
	Cfg    *config.Config // App configuration (needed for?) - maybe not needed here directly
}

// NewTableHandler creates a new TableHandler.
func NewTableHandler(metaDB *sql.DB, cfg *config.Config) *TableHandler {
	return &TableHandler{
		MetaDB: metaDB,
		Cfg:    cfg, // Pass config if needed later
	}
}

// --- Helper for common auth check and user DB connection ---
// Similar to RecordHandler's helper
func (h *TableHandler) checkScopeAndGetUserDB(c *gin.Context) (*sql.DB, string, error) {
	authUserID := c.MustGet("userId").(string)
	authDatabaseIDValue, _ := c.Get("databaseId") // nil if JWT/user-key
	targetDbName := c.Param("db_name")

	if !core.IsValidIdentifier(targetDbName) {
		return nil, "", fmt.Errorf("%w: invalid database name in URL path", nebulaErrors.ErrBadRequest) // Use defined error type
	}

	// Verify user owns the target DB and get its actual ID
	targetDatabaseID, err := storage.FindDatabaseIDByNameAndUser(c.Request.Context(), h.MetaDB, authUserID, targetDbName)
	if err != nil {
		// Propagate ErrDatabaseNotFound or other DB errors
		return nil, "", err
	}

	// If using a DB-scoped key, ensure it matches the target DB
	if authDatabaseIDValue != nil {
		authDatabaseID, ok := authDatabaseIDValue.(int64)
		if !ok { // Should not happen
			customLog.Warnf("ERROR: Invalid databaseID type in context for UserID %s", authUserID)
			return nil, "", fmt.Errorf("%w: internal authorization error", nebulaErrors.ErrInternalServer)
		}
		if authDatabaseID != targetDatabaseID {
			customLog.Warnf("Handler: FORBIDDEN - User %s API key for DBID %d attempted table operation on DB '%s' (ID %d)", authUserID, authDatabaseID, targetDbName, targetDatabaseID)
			return nil, "", fmt.Errorf("%w: API key not valid for database '%s'", nebulaErrors.ErrForbidden, targetDbName)
		}
	}
	// If JWT/user-key OR if DB-scoped key matches target, proceed

	// Get the file path using the confirmed user/dbName combo
	dbFilePath, err := storage.FindDatabasePath(c.Request.Context(), h.MetaDB, authUserID, targetDbName)
	if err != nil {
		// Should generally not happen if FindDatabaseIDByNameAndUser succeeded, but check anyway
		return nil, "", err
	}

	// Connect to the user's DB file
	userDB, err := storage.ConnectUserDB(c.Request.Context(), dbFilePath)
	if err != nil {
		return nil, "", err
	}

	// Return connection (caller must defer Close) and validated dbName

	return userDB, targetDbName, nil
}

// processSchemaRequest common logic for CreateSchema and CreateTable
func (h *TableHandler) processSchemaRequest(c *gin.Context, dbName, dbFilePath string) {
	var req models.CreateSchemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(fmt.Errorf("binding error: %w", err))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	if !core.IsValidIdentifier(req.TableName) {
		_ = c.Error(errors.New("invalid table name format"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid table name format."})
		return
	}

	// Support both Columns and Schema fields
	columns := req.Columns
	if len(columns) == 0 {
		columns = req.Schema
	}

	if len(columns) == 0 {
		_ = c.Error(errors.New("no columns provided"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "No columns provided in 'columns' or 'schema' field."})
		return
	}

	var columnDefs []string
	columnNames := make(map[string]bool)

	for _, col := range columns {
		colNameLower := strings.ToLower(col.Name)
		if !core.IsValidIdentifier(col.Name) || colNameLower == "id" {
			_ = c.Error(fmt.Errorf("invalid column name: %s", col.Name))
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid column name '%s'. Use valid identifiers, cannot be 'id'.", col.Name)})
			return
		}
		if columnNames[colNameLower] {
			_ = c.Error(fmt.Errorf("duplicate column name: %s", col.Name))
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Duplicate column name '%s'.", col.Name)})
			return
		}
		columnNames[colNameLower] = true

		normalizedType, ok := core.NormalizeAndValidateType(col.Type)
		if !ok {
			_ = c.Error(fmt.Errorf("invalid column type: %s", col.Type))
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid type '%s' for column '%s'.", col.Type, col.Name)})
			return
		}
		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", col.Name, normalizedType))
	}

	userDB, err := storage.ConnectUserDB(c.Request.Context(), dbFilePath)
	if err != nil {
		_ = c.Error(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()

	createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY AUTOINCREMENT, %s , created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);",
		req.TableName,
		strings.Join(columnDefs, ", "),
	)

	err = storage.CreateTable(c.Request.Context(), userDB, createTableSQL)
	if err != nil {
		_ = c.Error(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to create table."})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    fmt.Sprintf("Table '%s' created or already exists.", req.TableName),
		"db_name":    dbName,
		"table_name": req.TableName,
	})
}

// CreateTable handles requests to create a new table.
func (h *TableHandler) CreateTable(c *gin.Context) {
	userId := c.MustGet("userId").(string)
	dbName := c.Param("db_name")

	if !core.IsValidIdentifier(dbName) {
		_ = c.Error(errors.New("invalid db_name in path"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database name in URL path."})
		return
	}

	dbFilePath, err := storage.FindDatabasePath(c.Request.Context(), h.MetaDB, userId, dbName)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		}
		return
	}

	h.processSchemaRequest(c, dbName, dbFilePath)
}

// ListTables handles requests to list tables within a specific user database.
func (h *TableHandler) ListTablesFn(c *gin.Context) {
	userDb, dbName, err := h.checkScopeAndGetUserDB(c)
	if err != nil {
		_ = c.Error(err) // Let middleware handle response mapping
		return
	}
	defer userDb.Close()

	tables, err := storage.ListTables(c.Request.Context(), userDb)
	if err != nil {
		customLog.Warnf("Handler: Error listing tables for DB %s: %v", dbName, err)
		_ = c.Error(err)
		return
	}

	customLog.Printf("Handler: Retrieved %d table(s) for DB %s", len(tables), dbName)
	c.JSON(http.StatusOK, gin.H{"tables": tables})
}

// DeleteTable handles requests to drop a table within a specific user database.
func (h *TableHandler) DeleteTable(c *gin.Context) {
	targetTableName := c.Param("table_name") // Get table name from path

	// Validate table name format
	if !core.IsValidIdentifier(targetTableName) {
		err := fmt.Errorf("%w: invalid table name in URL path", nebulaErrors.ErrBadRequest)
		_ = c.Error(err)
		return
	}

	userDB, dbName, err := h.checkScopeAndGetUserDB(c) // Checks DB scope and connects
	if err != nil {
		_ = c.Error(err)
		return
	}
	defer userDB.Close()

	customLog.Printf("Handler: Attempting to drop table '%s' in DB '%s'", targetTableName, dbName)
	err = storage.DropTable(c.Request.Context(), userDB, targetTableName)
	if err != nil {
		// DropTable uses DROP IF EXISTS, so errors are likely more serious
		customLog.Warnf("Handler: Error dropping table '%s' in DB '%s': %v", targetTableName, dbName, err)
		_ = c.Error(err)
		return
	}

	customLog.Printf("Handler: Successfully dropped table '%s' in DB '%s'", targetTableName, dbName)

	c.Status(http.StatusNoContent) // Return 204 No Content on success
}
