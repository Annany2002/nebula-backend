// api/router.go
package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/Annany2002/nebula-backend/api/handlers"
	"github.com/Annany2002/nebula-backend/api/middleware" // Import middleware package
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/logger"
)

var (
	customLog = logger.NewLogger()
)

// SetupRouter initializes the Gin router and sets up all routes.
func SetupRouter(metaDB *sql.DB, cfg *config.Config) *gin.Engine {
	// Consider gin.New() for more control over default middleware

	router := gin.Default() // Includes Logger and Recovery

	// Configure CORS middleware
	err := godotenv.Load() // Loads .env file from current directory by default
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		customLog.Warnf("Warning: Error loading .env file: %v", err)
		// Decide if this should be a fatal error or just a warning
	}
	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	config := cors.DefaultConfig()
	config.AllowOrigins = strings.Split(allowedOrigins, " ")
	config.AllowMethods = []string{"POST", "OPTIONS", "GET", "PUT", "DELETE"} // Allows these methods.
	config.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"} // Allows these headers.
	router.Use(cors.New(config))
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
	tableHandler := handlers.NewTableHandler(metaDB, cfg)

	// --- Public Routes ---
	router.GET("/ping", func(c *gin.Context) { c.String(200, "pong") })

	// Public route for health check
	router.GET("/health", func(c *gin.Context) { c.Status(200) })
	authRoutes := router.Group("/auth")
	{ /* Routes using authHandler */
		authRoutes.POST("/signup", authHandler.Signup)
		authRoutes.POST("/login", authHandler.Login)
	}

	// Separate group for JWT-only protected routes ---
	// Example: Account management, API Key generation
	accountRoutes := router.Group("/api/v1/account")  // Or just /account
	accountRoutes.Use(middleware.AuthMiddleware(cfg)) // Use JWT Middleware HERE
	{
		accountRoutes.GET("/databases/:db_name/apikey", dbHandler.GetAPIKey)
		accountRoutes.POST("/databases/:db_name/apikey", dbHandler.CreateAPIKey)
		//      // Add other account routes here (e.g., change password)
	}

	// --- Protected Routes ---
	apiRoutes := router.Group("/api/v1")

	// Apply Combined Auth Middleware
	apiRoutes.Use(middleware.CombinedAuthMiddleware(metaDB, cfg))
	{ /* Routes using dbHandler and recordHandler */

		// health route to check for protected route health
		apiRoutes.GET("/health", func(c *gin.Context) {
			userId, okUserId := c.Get("userId")
			dbIDValue, okDbID := c.Get("databaseId")

			if !okUserId { // Should not happen if CombinedAuthMiddleware ran successfully
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "UserID not found in context after auth"})
				return
			}

			resp := gin.H{"userId": userId}
			// Check if okDbID is true AND dbIDValue is not nil
			if okDbID && dbIDValue != nil {
				// Attempt type assertion if not nil
				if dbID, ok := dbIDValue.(string); ok {
					resp["scope"] = fmt.Sprintf("database (ID: %v)", dbID)
				} else {
					// This case indicates an internal issue if databaseID was set but not int64
					resp["scope"] = "database (Error: unexpected type)"
					customLog.Printf("ERROR: Unexpected type for databaseID in context: %T", dbIDValue)
				}
			} else {
				// databaseID was explicitly set to nil (JWT) or wasn't set (shouldn't happen here)
				resp["scope"] = "user"
			}
			c.JSON(http.StatusOK, resp)
		})

		apiRoutes.GET("/user/:user_id", authHandler.FindUser)

		// Databases Manangement
		apiRoutes.GET("/databases", dbHandler.ListDatabases)
		apiRoutes.POST("/databases", dbHandler.CreateDatabase)
		apiRoutes.DELETE("/databases/:db_name", dbHandler.DeleteDatabase)

		// Schema Management
		apiRoutes.GET("/databases/:db_name/tables/:table_name/schema", dbHandler.GetSchema)
		apiRoutes.POST("/databases/:db_name/schema", dbHandler.CreateSchema)

		// Table Management
		apiRoutes.GET("/databases/:db_name/tables", tableHandler.ListTablesFn)
		apiRoutes.DELETE("/databases/:db_name/tables/:table_name", tableHandler.DeleteTable)

		// Record Management
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records", recordHandler.ListRecords)
		apiRoutes.POST("/databases/:db_name/tables/:table_name/records", recordHandler.CreateRecord)
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.GetRecord)
		apiRoutes.PUT("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.UpdateRecord)
		apiRoutes.DELETE("/databases/:db_name/tables/:table_name/records/:record_id", recordHandler.DeleteRecord)
	}

	return router
}
