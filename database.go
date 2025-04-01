// database.go
package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // Driver
)

// Global variable to hold the metadata database connection pool
var metaDB *sql.DB

// initMetadataDB initializes the connection to the metadata SQLite database
// and ensures the required tables exist.
func InitMetadataDB() (*sql.DB, error) {
	dbPath := filepath.Join(metadataDbDir, metadataDbFile) // Use constants from config.go
	log.Printf("Initializing metadata database: %s", dbPath)

	// Ensure the data directory exists
	if err := os.MkdirAll(metadataDbDir, 0750); err != nil {
		log.Printf("Error creating data directory '%s': %v", metadataDbDir, err)
		return nil, err
	}

	// Added ?_foreign_keys=on to enable foreign key constraint enforcement
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	log.Println("Metadata database connection successful.")

	// --- Create users table ---
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

	// --- *** NEW: Create databases table *** ---
	createDatabasesTableSQL := `
	CREATE TABLE IF NOT EXISTS databases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		db_name TEXT NOT NULL,
		file_path TEXT NOT NULL UNIQUE, -- File paths must be unique system-wide
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, db_name), -- A user cannot have two databases with the same name
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE -- If user is deleted, cascade delete their DB records
	);`
	_, err = db.Exec(createDatabasesTableSQL)
	if err != nil {
		log.Printf("Failed to create databases table: %v", err)
		db.Close() // Close connection if table creation fails
		return nil, err
	}
	log.Println("Databases table ensured.")
	// --- *** END NEW *** ---

	return db, nil
}
