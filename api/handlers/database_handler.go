// api/handlers/database_handler.go
package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/core"    // For validation
	"github.com/Annany2002/nebula-backend/internal/storage" // For DB operations
)

// DatabaseHandler holds dependencies for DB/Schema management handlers.
type DatabaseHandler struct {
	MetaDB *sql.DB        // Metadata DB pool
	Cfg    *config.Config // App configuration
	// UserRepo *storage.UserDBRepo // Could inject repo struct later
}

// NewDatabaseHandler creates a new DatabaseHandler.
func NewDatabaseHandler(metaDB *sql.DB, cfg *config.Config) *DatabaseHandler {
	return &DatabaseHandler{
		MetaDB: metaDB,
		Cfg:    cfg,
	}
}

// CreateDatabase handles requests to register a new user database.
func (h *DatabaseHandler) CreateDatabase(c *gin.Context) {
	userID := c.MustGet("userID").(int64) // From AuthMiddleware

	var req models.CreateDatabaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(fmt.Errorf("binding error: %w", err))                                                    // Use c.Error
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()}) // Temporary direct response
		return
	}

	if !core.IsValidIdentifier(req.DBName) {
		_ = c.Error(errors.New("invalid database name format"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database name. Use only alphanumeric characters and underscores (a-z, A-Z, 0-9, _), max length 64."})
		return
	}

	// Construct file path
	userDbDir := filepath.Join(h.Cfg.MetadataDbDir, fmt.Sprintf("%d", userID))
	dbFilePath := filepath.Join(userDbDir, req.DBName+".db")

	// Ensure user directory exists (moved from handler to make it more reusable?)
	// Or keep it here as it's tied to the registration action. Let's keep it here.
	if err := os.MkdirAll(userDbDir, 0750); err != nil {
		log.Printf("Create DB: Error creating user DB directory '%s': %v", userDbDir, err)
		_ = c.Error(fmt.Errorf("storage setup error: %w", err))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to create database storage location"})
		return
	}

	// Register in metadata DB using storage function
	err := storage.RegisterDatabase(c.Request.Context(), h.MetaDB, userID, req.DBName, dbFilePath)
	if err != nil {
		_ = c.Error(err) // Pass storage error to context
		if errors.Is(err, storage.ErrDatabaseExists) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "A database with this name already exists."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to register database."})
		}
		return
	}

	log.Printf("Handler: Successfully registered database '%s' for UserID %d", req.DBName, userID)
	c.JSON(http.StatusCreated, gin.H{
		"message": "Database registered successfully",
		"db_name": req.DBName,
	})
}

// CreateSchema handles requests to define a table schema.
func (h *DatabaseHandler) CreateSchema(c *gin.Context) {
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name")

	if !core.IsValidIdentifier(dbName) {
		_ = c.Error(errors.New("invalid db_name in path"))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid database name in URL path."})
		return
	}

	// Look up path via storage function
	dbFilePath, err := storage.FindDatabasePath(c.Request.Context(), h.MetaDB, userID, dbName)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		}
		return
	}

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

	var columnDefs []string
	columnNames := make(map[string]bool) // Check for duplicate column names
	for _, col := range req.Columns {
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
		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", col.Name, normalizedType)) // Use original name case
	}

	// Connect to the user DB using storage function
	userDB, err := storage.ConnectUserDB(c.Request.Context(), dbFilePath)
	if err != nil {
		_ = c.Error(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()

	// Construct CREATE TABLE SQL
	// Use validated table name and column definitions
	createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY AUTOINCREMENT, %s);",
		req.TableName, // Already validated
		strings.Join(columnDefs, ", "),
	)
	log.Printf("Handler: Executing Schema SQL for UserID %d, DB '%s': %s", userID, dbName, createTableSQL)

	// Execute via storage function
	err = storage.CreateTable(c.Request.Context(), userDB, createTableSQL)
	if err != nil {
		_ = c.Error(err)
		// Could inspect err further if CreateTable returned more specific errors
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to create table."})
		return
	}

	log.Printf("Handler: Successfully ensured table '%s' in DB '%s' for UserID %d", req.TableName, dbName, userID)
	c.JSON(http.StatusCreated, gin.H{
		"message":    fmt.Sprintf("Table '%s' created or already exists.", req.TableName),
		"db_name":    dbName,
		"table_name": req.TableName,
	})
}

// --- *** NEW: List Databases Handler *** ---

// ListDatabases handles requests to list registered databases for the user.
func (h *DatabaseHandler) ListDatabases(c *gin.Context) {
	userID := c.MustGet("userID").(int64) // From AuthMiddleware

	dbNames, err := storage.ListUserDatabases(c.Request.Context(), h.MetaDB, userID)
	if err != nil {
		log.Printf("Handler: Error listing databases for UserID %d: %v", userID, err)
		_ = c.Error(err) // Attach storage error
		// Let middleware handle response (likely 500)
		return
	}

	log.Printf("Handler: Retrieved %d database(s) for UserID %d", len(dbNames), userID)
	c.JSON(http.StatusOK, gin.H{"databases": dbNames})
}

// --- *** END NEW *** ---

// --- *** NEW: List Tables Handler *** ---

// ListTables handles requests to list tables within a specific user database.
func (h *DatabaseHandler) ListTables(c *gin.Context) {
	// Need to connect to the specific user DB first
	// Reusing logic similar to RecordHandler's getUserDBConn helper idea
	userID := c.MustGet("userID").(int64)
	dbName := c.Param("db_name")

	if !core.IsValidIdentifier(dbName) {
		err := errors.New("invalid database name in URL path")
		_ = c.Error(err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()}) // Temp direct
		return
	}

	// Find path, connect to user DB
	dbFilePath, err := storage.FindDatabasePath(c.Request.Context(), h.MetaDB, userID, dbName)
	if err != nil {
		_ = c.Error(err)
		if errors.Is(err, storage.ErrDatabaseNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Database not found or not registered."})
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve database information."})
		}
		return
	}
	userDB, err := storage.ConnectUserDB(c.Request.Context(), dbFilePath)
	if err != nil {
		_ = c.Error(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to access database storage."})
		return
	}
	defer userDB.Close()

	// Call storage function to list tables
	tableNames, err := storage.ListTables(c.Request.Context(), userDB)
	if err != nil {
		log.Printf("Handler: Error listing tables for UserID %d, DB %s: %v", userID, dbName, err)
		_ = c.Error(err)
		// Let middleware handle (likely 500)
		return
	}

	log.Printf("Handler: Retrieved %d table(s) for UserID %d, DB %s", len(tableNames), userID, dbName)
	c.JSON(http.StatusOK, gin.H{"tables": tableNames})
}

// --- *** END NEW ---
