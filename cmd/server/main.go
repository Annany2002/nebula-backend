// cmd/server/main.go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/Annany2002/nebula-backend/api"              // Import router setup
	"github.com/Annany2002/nebula-backend/config"           // Import config loading
	"github.com/Annany2002/nebula-backend/internal/storage" // Import DB connection func
)

func main() {
	log.Println("Starting Nebula Backend server...")

	// 1. Load Configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// 2. Initialize Metadata Database Connection
	metaDB, err := storage.ConnectMetadataDB(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize metadata database: %v", err)
		os.Exit(1)
	}
	defer func() {
		log.Println("Closing metadata database connection...")
		if err := metaDB.Close(); err != nil {
			log.Printf("Error closing metadata database: %v", err)
		}
	}()

	// 3. Setup Router (passing dependencies)
	router := api.SetupRouter(metaDB, cfg)

	// 4. Start Server
	log.Printf("Server listening on port %s", cfg.ServerPort)
	if err := router.Run(fmt.Sprintf(":%s", cfg.ServerPort)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
