package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("Starting Nebula server...")

	// Initialize metadata database connection
	var err error
	metaDB, err = InitMetadataDB() // Call function from database.go
	if err != nil {
		log.Fatalf("Failed to initialize metadata database: %v", err)
		os.Exit(1)
	}
	// Ensure the database connection is closed when main exits
	if metaDB != nil {
		defer metaDB.Close()
		log.Println("Metadata database connection deferred close.")
	}

	// Initialize Gin router
	router := gin.Default()

	// --- Public Routes ---
	router.GET("/ping", func(c *gin.Context) {
		// Optional: Check DB ping
		err := metaDB.Ping()
		status := http.StatusOK
		message := "pong"
		if err != nil {
			status = http.StatusInternalServerError
			message = "pong, but DB connection error"
			log.Printf("DB Ping error during /ping request: %v", err)
		}
		c.JSON(status, gin.H{
			"message": message,
		})
	})

	// Auth Group (Public) - Handlers are in auth.go
	authRoutes := router.Group("/auth")
	{
		authRoutes.POST("/signup", signupHandler)
		authRoutes.POST("/login", loginHandler)
	}

	// --- Protected Routes ---
	// Middleware is in auth.go
	apiRoutes := router.Group("/api/v1")
	apiRoutes.Use(AuthMiddleware()) // Apply middleware from auth.go
	{
		// Test endpoint - Handler defined inline for simplicity here, could be moved
		apiRoutes.GET("/me", func(c *gin.Context) {
			userID := c.MustGet("userID").(int64) // Get userID from context
			c.JSON(http.StatusOK, gin.H{
				"message": "This is a protected route",
				"userID":  userID,
			})
		})

		// Database & Schema Management
		apiRoutes.POST("/databases", createDatabaseHandler)
		apiRoutes.POST("/databases/:db_name/schema", createSchemaHandler)

		// Record CRUD
		apiRoutes.POST("/databases/:db_name/tables/:table_name/records", createRecordHandler)
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records", listRecordsHandler)

		// --- *** NEW: Register Get Single Record route *** ---
		// Handler is in db_handlers.go
		apiRoutes.GET("/databases/:db_name/tables/:table_name/records/:record_id", getRecordHandler)
		// --- *** END NEW *** ---

		// Update route will go here next:
		// PUT     /databases/:db_name/tables/:table_name/records/:record_id
		// ... Delete ...

	}

	// Start the server
	port := ":8080" // TODO: Make port configurable (e.g., from config.go or env var)
	log.Printf("Server listening on port %s", port)
	if err := router.Run(port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
