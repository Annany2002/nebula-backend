// api/handlers/table_handler.go
package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

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
