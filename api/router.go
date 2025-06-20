// api/router.go
package api

import (
	"database/sql"
	"errors"
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
	// Login, Signup routes
	authRoutes := router.Group("/auth")
	{ /* Routes using authHandler */
		authRoutes.POST("/signup", authHandler.Signup)
		authRoutes.POST("/login", authHandler.Login)
	}

	// Separate group for JWT-only protected routes ---
	// Example: Account management, API Key generation
	accountRoutes := router.Group("/api/v1/account")
	accountRoutes.Use(middleware.AuthMiddleware(cfg))
	{
		accountRoutes.GET("/databases/:db_name/apikey", dbHandler.GetAPIKey)
		accountRoutes.POST("/databases/:db_name/apikey", dbHandler.CreateAPIKey)
		accountRoutes.DELETE("/databases/:db_name/apikey", dbHandler.DeleteAPIKey)
	}

	// --- Protected Routes ---
	apiRoutes := router.Group("/api/v1")

	// Apply Combined Auth Middleware
	apiRoutes.Use(middleware.CombinedAuthMiddleware(metaDB, cfg))
	{ /* Routes using dbHandler and recordHandler */

		// health route to check for protected route health
		apiRoutes.GET("/health", func(c *gin.Context) {
			userId, okUserId := c.Get("userId")
			dbIDValue, _ := c.Get("databaseId")
			_, okApiAuth := c.Get("isApiKey")

			if !okUserId { // Should not happen if CombinedAuthMiddleware ran successfully
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "UserID not found in context after auth"})
				return
			}

			// api key auth
			if okApiAuth {
				c.JSON(http.StatusOK, gin.H{"authenticated_by": "api_key", "status": "ok"})
				return
			}

			c.JSON(http.StatusOK, gin.H{"userId": userId, "dbId": dbIDValue})
		})

		apiRoutes.GET("/user/:user_id", authHandler.FindUser)
		// apiRoutes.GET("/user/me", authHandler.GetUser)

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
