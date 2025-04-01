package main

import (
	"database/sql"
	"log"
	"os"            // Import os
	"path/filepath" // Import filepath

	_ "github.com/mattn/go-sqlite3" // Driver
)

// Global variable to hold the metadata database connection pool
var metaDB *sql.DB

// initMetadataDB initializes the connection to the metadata SQLite database
// and ensures the required tables exist.
func InitMetadataDB() (*sql.DB, error) {
	dbPath := filepath.Join(metadataDbDir, metadataDbFile)
	log.Printf("Initializing metadata database: %s", dbPath)

	// Ensure the data directory exists
	if err := os.MkdirAll(metadataDbDir, 0750); err != nil { // 0750 permissions
		log.Printf("Error creating data directory '%s': %v", metadataDbDir, err)
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on") // Use path from config
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	log.Println("Metadata database connection successful.")

	// SQL statement to create the users table if it doesn't exist
	createUsersTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	_, err = db.Exec(createUsersTableSQL)
	if err != nil {
		log.Printf("Failed to create users table: %v", err)
		db.Close()
		return nil, err
	}
	log.Println("Users table ensured.")

	// --- Placeholder for databases table ---
	// Later: Add databases table creation here

	return db, nil
}
