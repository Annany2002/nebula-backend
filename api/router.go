// api/router.go
package api

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/handlers"   // Import handlers
	"github.com/Annany2002/nebula-backend/api/middleware" // Import middleware
	"github.com/Annany2002/nebula-backend/config"         // Import config
)

// SetupRouter initializes the Gin router and sets up all routes.
// It requires dependencies like the database pool and config.
func SetupRouter(metaDB *sql.DB, cfg *config.Config) *gin.Engine {
	router := gin.Default() // Or gin.New() if you want more control

	// --- Initialize Handlers (with dependencies) ---
	authHandler := handlers.NewAuthHandler(metaDB, cfg)
	dbHandler := handlers.NewDatabaseHandler(metaDB, cfg)
	recordHandler := handlers.NewRecordHandler(metaDB, cfg)

	// --- Global Middleware (Example) ---
	// router.Use(gin.Logger())
	// router.Use(gin.Recovery())
	// router.Use(middleware.CORSMiddleware()) // Example CORS

	// --- Error Handling Middleware (to be implemented later) ---
	// router.Use(middleware.ErrorHandler())

	// --- Public Routes ---
	router.GET("/ping", func(c *gin.Context) {
		if err := metaDB.Ping(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "pong, but DB error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	authRoutes := router.Group("/auth")
	{
		authRoutes.POST("/signup", authHandler.Signup) // Use method from handler struct
		authRoutes.POST("/login", authHandler.Login)   // Use method from handler struct
	}

	// --- Protected Routes ---
	apiRoutes := router.Group("/api/v1")
	apiRoutes.Use(middleware.AuthMiddleware(cfg)) // Apply auth middleware
	{
		// Example 'me' route - could be moved to a user handler
		apiRoutes.GET("/me", func(c *gin.Context) {
			userID, exists := c.Get("userID")
			if !exists {
				// Should ideally be caught by auth middleware, but double check
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "UserID not found in context"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"userID": userID})
		})

		// Database & Schema Management
		apiRoutes.POST("/databases", dbHandler.CreateDatabase)               // Use method
		apiRoutes.POST("/databases/:db_name/schema", dbHandler.CreateSchema) // Use method

		// Record CRUD
		apiRoutes.POST("/databases/:db_name/tables/:table_name/records", recordHandler.CreateRecord)              // Use method
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records", recordHandler.ListRecords)                // Use method
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.GetRecord)       // Use method
		apiRoutes.PUT("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.UpdateRecord)    // Use method
		apiRoutes.DELETE("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.DeleteRecord) // Use method
	}

	return router
}
