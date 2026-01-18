// internal/storage/database.go
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // Driver registration

	"github.com/Annany2002/nebula-backend/config" // Import config package
	"github.com/Annany2002/nebula-backend/internal/logger"
)

var (
	customLog = logger.NewLogger()
)

// ConnectMetadataDB initializes the connection pool for the metadata SQLite database
// and ensures the required tables ('users', 'databases', 'api_key') exist.
func ConnectMetadataDB(cfg *config.Config) (*sql.DB, error) {
	dbPath := filepath.Join(cfg.MetadataDbDir, cfg.MetadataDbFile)
	customLog.Printf("Storage: Initializing metadata database: %s", dbPath)

	// Ensure the data directory exists
	if err := os.MkdirAll(cfg.MetadataDbDir, 0o750); err != nil {
		customLog.Warnf("Storage: Error creating data directory '%s': %v", cfg.MetadataDbDir, err)
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Adding ?_foreign_keys=on to enable foreign key constraint enforcement and adding pragmas WAL mode and busy timeout for 5s if db is busy
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		customLog.Warnf("Storage: Failed to open metadata db '%s': %v", dbPath, err)
		return nil, fmt.Errorf("failed to open metadata db: %w", err)
	}

	// Verify connection is working
	if err = db.Ping(); err != nil {
		db.Close() // Close the connection if ping fails
		customLog.Warnf("Storage: Failed to ping metadata db '%s': %v", dbPath, err)
		return nil, fmt.Errorf("failed to connect to metadata db: %w", err)
	}
	customLog.Println("Storage: Metadata database connection successful.")

	// --- Ensure 'users' table exists ---
	createUsersTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		user_id TEXT PRIMARY KEY UNIQUE NOT NULL,
		username TEXT NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err = db.Exec(createUsersTableSQL); err != nil {
		db.Close()
		customLog.Warnf("Storage: Failed to create users table: %v", err)
		return nil, fmt.Errorf("failed to ensure users table: %w", err)
	}
	customLog.Println("Storage: Users table ensured.")

	// --- Ensure 'databases' table exists ---
	createDatabasesTableSQL := `
	CREATE TABLE IF NOT EXISTS databases (
		database_id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL,
		db_name TEXT NOT NULL,
		file_path TEXT UNIQUE NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (owner_id, db_name),
		FOREIGN KEY (owner_id) REFERENCES users(user_id) ON DELETE CASCADE
	);`
	if _, err = db.Exec(createDatabasesTableSQL); err != nil {
		db.Close()
		customLog.Warnf("Storage: Failed to create databases table: %v", err)
		return nil, fmt.Errorf("failed to ensure databases table: %w", err)
	}
	customLog.Println("Storage: Databases table ensured.")

	// Configure connection pool settings (optional but recommended)
	// db.SetMaxOpenConns(25)
	// db.SetMaxIdleConns(5)
	// db.SetConnMaxLifetime(5*time.Minute)

	// Ensure 'api_keys' table  ---
	// nolint:gosec // G101 false positive - this is table schema, not hardcoded credentials
	createAPIKeysTableSQL := `
	CREATE TABLE IF NOT EXISTS api_keys (
		api_key_id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_owner_id TEXT NOT NULL,
		api_database_id INTEGER UNIQUE NOT NULL,
		key TEXT UNIQUE NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_owner_id) REFERENCES users(user_id) ON DELETE CASCADE,
		FOREIGN KEY (api_database_id) REFERENCES databases(database_id) ON DELETE CASCADE
	);`
	if _, err = db.Exec(createAPIKeysTableSQL); err != nil {
		db.Close()
		customLog.Printf("Storage: Failed to create api_keys table: %v", err)
		return nil, fmt.Errorf("failed to ensure api_keys table: %w", err)
	}

	customLog.Println("Storage: API Keys table ensured.")

	return db, nil
}
