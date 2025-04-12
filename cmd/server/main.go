// cmd/server/main.go
package main

import (
	"fmt"
	"os"

	"github.com/Annany2002/nebula-backend/api"    // Import router setup
	"github.com/Annany2002/nebula-backend/config" // Import config loading
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage" // Import DB connection func
)

var (
	customLog = logger.NewLogger()
)

func main() {
	customLog.Println("Starting Nebula Backend server...")

	// 1. Load Configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		customLog.Fatalf("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// 2. Initialize Metadata Database Connection
	metaDB, err := storage.ConnectMetadataDB(cfg)
	if err != nil {
		customLog.Fatalf("Failed to initialize metadata database: %v", err)
		os.Exit(1)
	}
	defer func() {
		customLog.Println("Closing metadata database connection...")
		if err := metaDB.Close(); err != nil {
			customLog.Printf("Error closing metadata database: %v", err)
		}
	}()

	// 3. Setup Router (passing dependencies)
	router := api.SetupRouter(metaDB, cfg)

	// 4. Start Server
	customLog.Printf("Server listening on port %s", cfg.ServerPort)
	if err := router.Run(fmt.Sprintf(":%s", cfg.ServerPort)); err != nil {
		customLog.Fatalf("Failed to start server: %v", err)
	}
}
