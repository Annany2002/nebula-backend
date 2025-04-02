// api/router.go
package api

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/handlers"
	"github.com/Annany2002/nebula-backend/api/middleware" // Import middleware package
	"github.com/Annany2002/nebula-backend/config"
)

// SetupRouter initializes the Gin router and sets up all routes.
func SetupRouter(metaDB *sql.DB, cfg *config.Config) *gin.Engine {
	// Consider gin.New() for more control over default middleware
	router := gin.Default() // Includes Logger and Recovery

	// Setting up a rate-limiter
	ratelimiter := middleware.NewRateLimiter()
	router.Use(middleware.RateLimitMiddleware(ratelimiter))
	// It should run after basic middleware like Logger/Recovery
	// but before the routing happens, so it wraps the handlers.
	router.Use(middleware.ErrorHandler())

	// Initialize Handlers
	authHandler := handlers.NewAuthHandler(metaDB, cfg)
	dbHandler := handlers.NewDatabaseHandler(metaDB, cfg)
	recordHandler := handlers.NewRecordHandler(metaDB, cfg)

	// --- Public Routes ---
	router.GET("/ping", func(c *gin.Context) { c.String(200, "Hello World") })
	authRoutes := router.Group("/auth")
	{ /* Routes using authHandler */
		authRoutes.POST("/signup", authHandler.Signup)
		authRoutes.POST("/login", authHandler.Login)
	}

	// --- Protected Routes ---
	apiRoutes := router.Group("/api/v1")
	// Apply AuthMiddleware first for protected routes
	apiRoutes.Use(middleware.AuthMiddleware(cfg))
	{ /* Routes using dbHandler and recordHandler */
		apiRoutes.GET("/me", func(c *gin.Context) { /* ... */ })

		apiRoutes.GET("/databases", dbHandler.ListDatabases)
		apiRoutes.POST("/databases", dbHandler.CreateDatabase)

		apiRoutes.POST("/databases/:db_name/schema", dbHandler.CreateSchema)

		apiRoutes.GET("/databases/:db_name/tables", dbHandler.ListTables)

		// *** NEW: Delete Table route ***
		apiRoutes.DELETE("/databases/:db_name/tables/:table_name", dbHandler.DeleteTable)
		// *** END NEW ***

		// *** NEW: Delete Database route ***
		apiRoutes.DELETE("/databases/:db_name", dbHandler.DeleteDatabase)
		// *** END NEW ***

		apiRoutes.POST("/databases/:db_name/tables/:table_name/records", recordHandler.CreateRecord)
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records", recordHandler.ListRecords)
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.GetRecord)
		apiRoutes.PUT("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.UpdateRecord)
		apiRoutes.DELETE("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.DeleteRecord)
	}

	return router
}
