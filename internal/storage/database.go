// internal/storage/database.go
package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // Driver registration

	"github.com/Annany2002/nebula-backend/config" // Import config package
)

// ConnectMetadataDB initializes the connection pool for the metadata SQLite database
// and ensures the required tables ('users', 'databases') exist.
func ConnectMetadataDB(cfg *config.Config) (*sql.DB, error) {
	dbPath := filepath.Join(cfg.MetadataDbDir, cfg.MetadataDbFile)
	log.Printf("Storage: Initializing metadata database: %s", dbPath)

	// Ensure the data directory exists
	if err := os.MkdirAll(cfg.MetadataDbDir, 0750); err != nil {
		log.Printf("Storage: Error creating data directory '%s': %v", cfg.MetadataDbDir, err)
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Added ?_foreign_keys=on to enable foreign key constraint enforcement
	// Consider adding other pragmas like WAL mode if needed: "?_foreign_keys=on&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		log.Printf("Storage: Failed to open metadata db '%s': %v", dbPath, err)
		return nil, fmt.Errorf("failed to open metadata db: %w", err)
	}

	// Verify connection is working
	if err = db.Ping(); err != nil {
		db.Close() // Close the connection if ping fails
		log.Printf("Storage: Failed to ping metadata db '%s': %v", dbPath, err)
		return nil, fmt.Errorf("failed to connect to metadata db: %w", err)
	}
	log.Println("Storage: Metadata database connection successful.")

	// --- Ensure 'users' table exists ---
	createUsersTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err = db.Exec(createUsersTableSQL); err != nil {
		db.Close()
		log.Printf("Storage: Failed to create users table: %v", err)
		return nil, fmt.Errorf("failed to ensure users table: %w", err)
	}
	log.Println("Storage: Users table ensured.")

	// --- Ensure 'databases' table exists ---
	createDatabasesTableSQL := `
	CREATE TABLE IF NOT EXISTS databases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		db_name TEXT NOT NULL,
		file_path TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, db_name),
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);`
	if _, err = db.Exec(createDatabasesTableSQL); err != nil {
		db.Close()
		log.Printf("Storage: Failed to create databases table: %v", err)
		return nil, fmt.Errorf("failed to ensure databases table: %w", err)
	}
	log.Println("Storage: Databases table ensured.")

	// Configure connection pool settings (optional but recommended)
	// db.SetMaxOpenConns(25)
	// db.SetMaxIdleConns(5)
	// db.SetConnMaxLifetime(5*time.Minute)

	return db, nil
}
