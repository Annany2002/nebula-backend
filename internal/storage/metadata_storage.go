// internal/storage/metadata_repo.go
package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/Annany2002/nebula-backend/internal/domain"
	"github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// Specific errors for metadata operations
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailExists        = errors.New("email already exists")
	ErrDatabaseExists     = errors.New("database name already exists for this user")
	ErrDatabaseNotFound   = errors.New("database not found or not registered for this user")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrConflict           = errors.New("constraint violation")
	ErrAPIKeyGeneration   = errors.New("failed to generate api key components")
)

const apiKeyPrefix = "neb_"   // Prefix for nebula
const apiKeySecretLength = 32 // Length of the random secret part in bytes

// Define a struct to hold key details for validation
type APIKeyData struct {
	UserID     string
	DatabaseID int64
	HashedKey  string
}

// --- User Operations ---

// CreateUser inserts a new user into the metadata database.
func CreateUser(ctx context.Context, db *sql.DB, user_id, username, email, passwordHash string) (string, error) {
	sqlStatement := `INSERT INTO users (user_id, username, email, password_hash) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, sqlStatement, user_id, username, email, passwordHash)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			if strings.Contains(sqliteErr.Error(), "users.email") {
				return "", ErrEmailExists
			}
		}
		customLog.Warnf("Storage: Failed to insert user %s: %v", email, err)
		return "", fmt.Errorf("database error during user creation: %w", err)
	}

	return user_id, nil
}

// FindUserByEmail retrieves a user by their email address.
func FindUserByEmail(ctx context.Context, db *sql.DB, email string) (*domain.UserMetadata, error) {
	sqlStatement := `SELECT * FROM users WHERE email = ? LIMIT 1`
	row := db.QueryRowContext(ctx, sqlStatement, email)

	var user domain.UserMetadata
	err := row.Scan(&user.UserId, &user.Username, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		customLog.Warnf("Storage: Failed to find user by email %s: %v", email, err)
		return nil, fmt.Errorf("database error finding user: %w", err)
	}
	return &user, nil
}

// FindUserByUserId finds a user with user_id
func FindUserByUserId(ctx context.Context, db *sql.DB, user_id string) (*domain.UserMetadata, error) {
	sqlStatement := `SELECT * FROM users WHERE user_id = ? LIMIT 1`
	row := db.QueryRowContext(ctx, sqlStatement, user_id)

	var user domain.UserMetadata
	err := row.Scan(&user.UserId, &user.Username, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		customLog.Warnf("Storage: Failed to find user by user_id %s: %v", user_id, err)
		return nil, fmt.Errorf("database error finding user: %w", err)
	}
	return &user, nil
}

// --- Database Registration Operations ---

// RegisterDatabase inserts a new database registration record.
func RegisterDatabase(ctx context.Context, db *sql.DB, userId, dbName, filePath string) error {
	sqlStatement := `INSERT INTO databases (owner_id, db_name, file_path) VALUES (?, ?, ?)`
	_, err := db.ExecContext(ctx, sqlStatement, userId, dbName, filePath)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			// Could be UNIQUE(user_id, db_name) or UNIQUE(file_path)
			customLog.Warnf("Storage: Constraint violation registering DB '%s' for user %s: %v", dbName, userId, err)
			return ErrDatabaseExists // Assume name conflict for user
		}
		customLog.Warnf("Storage: Failed to insert database record for UserID %s, DBName '%s': %v", userId, dbName, err)
		return fmt.Errorf("database error registering database: %w", err)
	}
	return nil
}

// FindDatabasePath retrieves the file path for a given user and database name.
func FindDatabasePath(ctx context.Context, db *sql.DB, userId, dbName string) (string, error) {
	var dbFilePath string

	lookupSQL := `SELECT file_path FROM databases WHERE owner_id = ? AND db_name = ? LIMIT 1`
	err := db.QueryRowContext(ctx, lookupSQL, userId, dbName).Scan(&dbFilePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrDatabaseNotFound
		}
		customLog.Warnf("Storage: Error looking up database path for UserID %s, DBName '%s': %v", userId, dbName, err)
		return "", fmt.Errorf("database error finding database path: %w", err)
	}
	return dbFilePath, nil
}

// ListUserDatabases retrieves a list of database names registered by a specific user.
func ListUserDatabases(ctx context.Context, db *sql.DB, userId string) ([]domain.DatabaseMetadata, error) {
	query := `SELECT * FROM databases WHERE owner_id = ? ORDER BY db_name;`
	rows, err := db.QueryContext(ctx, query, userId)
	if err != nil {
		customLog.Warnf("Storage: Error listing databases for UserID %s: %v", userId, err)
		return nil, fmt.Errorf("database error listing databases: %w", err)
	}
	defer rows.Close()

	var userDb []domain.DatabaseMetadata

	for rows.Next() {
		var singleDb domain.DatabaseMetadata
		if err := rows.Scan(&singleDb.DatabaseID, &singleDb.UserID, &singleDb.DBName, &singleDb.FilePath, &singleDb.CreatedAt); err != nil {
			customLog.Warnf("Storage: Error scanning database name for UserID %s: %v", userId, err)
			return nil, fmt.Errorf("failed processing database list: %w", err)
		}

		userSingleDb, err := ConnectUserDB(ctx, singleDb.FilePath)
		if err != nil {
			customLog.Warnf("Error opening database %s of user %s", singleDb.DBName, userId)
			continue
			// return nil, ErrTableNotFound
		}
		defer userSingleDb.Close()

		err = userSingleDb.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table';").Scan(&singleDb.Tables)
		if err != nil {
			customLog.Printf("Error counting tables in %s: %v\n", singleDb.FilePath, err)
			continue // Skip to the next database
		}

		userDb = append(userDb, singleDb)
	}
	if err = rows.Err(); err != nil {
		customLog.Warnf("Storage: Error iterating database list for UserID %s: %v", userId, err)
		return nil, fmt.Errorf("failed reading database list: %w", err)
	}

	// Return empty slice if user has no databases, not an error
	if userDb == nil {
		userDb = make([]domain.DatabaseMetadata, 0)
	}
	return userDb, nil
}

// DeleteDatabaseRegistration removes the database entry from the metadata table.
// It returns ErrDatabaseNotFound if no matching entry was found.
func DeleteDatabaseRegistration(ctx context.Context, db *sql.DB, userId, dbName string) error {
	deleteSQL := `DELETE FROM databases WHERE owner_id = ? AND db_name = ?;`
	result, err := db.ExecContext(ctx, deleteSQL, userId, dbName)
	if err != nil {
		// Likely a connection or syntax issue, not "not found"
		customLog.Warnf("Storage: Error executing delete registration for UserID %s, DB '%s': %v", userId, dbName, err)
		return fmt.Errorf("database error deleting registration: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		customLog.Warnf("Storage: Error getting RowsAffected for delete registration UserID %s, DB '%s': %v", userId, dbName, err)
		return fmt.Errorf("failed confirming registration deletion: %w", err)
	}

	if rowsAffected == 0 {
		// No rows matched the user_id and db_name combination
		return ErrDatabaseNotFound
	}

	return nil // Success
}

// FindDatabaseIDByNameAndUser retrieves the ID of a database owned by a specific user.
// Returns the database ID or ErrDatabaseNotFound if no match.
func FindDatabaseIDByNameAndUser(ctx context.Context, db *sql.DB, userId string, dbName string) (int64, error) {
	var databaseId int64
	query := `SELECT database_id FROM databases WHERE owner_id = ? AND db_name = ? LIMIT 1;`
	err := db.QueryRowContext(ctx, query, userId, dbName).Scan(&databaseId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrDatabaseNotFound // Specific error
		}
		customLog.Warnf("Storage: Error finding database ID for UserID %s, DB '%s': %v", userId, dbName, err)
		return 0, fmt.Errorf("database error finding database ID: %w", err)
	}
	return databaseId, nil
}

// generateAPIKeyParts creates a new prefix, secret, and securely hashes the secret.
func generateAPIKeyParts() (prefix string, secret string, hashedSecret string, err error) {
	prefix = apiKeyPrefix
	// Generate cryptographically secure random bytes for the secret
	randomBytes := make([]byte, apiKeySecretLength)
	_, err = rand.Read(randomBytes)
	if err != nil {
		customLog.Warnf("Storage: Failed to generate random bytes for API key: %v", err)
		return "", "", "", ErrAPIKeyGeneration
	}
	// Encode random bytes to a URL-safe base64 string for the secret part
	secret = base64.RawURLEncoding.EncodeToString(randomBytes)

	// Hash the generated secret using bcrypt
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		customLog.Warnf("Storage: Failed to hash API key secret: %v", err)
		return "", "", "", ErrAPIKeyGeneration
	}
	hashedSecret = string(hashedBytes)

	return prefix, secret, hashedSecret, nil
}

// StoreAPIKey generates and stores a new API key scoped to a specific user and database.
// It returns the *full, unhashed* key (prefix + secret) ONCE upon successful creation.
func StoreAPIKey(ctx context.Context, db *sql.DB, userId string, databaseID int64, label string) (string, error) {
	if label == "" {
		return "", errors.New("API key label cannot be empty") // Basic validation
	}

	prefix, secret, hashedSecret, err := generateAPIKeyParts()
	if err != nil {
		return "", err // Return generation error
	}

	// Store the prefix, HASHED secret, and other details in the DB
	insertSQL := `INSERT INTO api_keys (api_owner_id, database_id, key_prefix, hashed_key, label) VALUES (?, ?, ?, ?, ?);`
	_, err = db.ExecContext(ctx, insertSQL, userId, databaseID, prefix, hashedSecret, label)
	if err != nil {
		// Handle potential constraint violations (e.g., UNIQUE on hashed_key, though collisions are extremely unlikely)
		customLog.Warnf("Storage: Failed to store API key for UserID %v, DBID %d: %v", userId, databaseID, err)
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			return "", ErrConflict
		}
		return "", fmt.Errorf("database error storing API key: %w", err)
	}

	// Return the full, unhashed key (prefix + secret) - ONLY this one time!
	fullAPIKey := prefix + secret
	return fullAPIKey, nil
}

// FindAPIKeysByPrefix retrieves potential key data matching a given prefix.
// In a real high-traffic system, querying just by prefix might be inefficient
// or insecure if prefixes are not unique enough. Consider indexing key_prefix.
func FindAPIKeysByPrefix(ctx context.Context, db *sql.DB, prefix string) ([]APIKeyData, error) {
	query := `SELECT api_owner_id, database_id, hashed_key FROM api_keys WHERE key_prefix = ?;`
	rows, err := db.QueryContext(ctx, query, prefix)
	if err != nil {
		customLog.Warnf("Storage: Error querying API keys by prefix '%s': %v", prefix, err)
		// Don't return specific errors like Not Found here, let middleware handle empty results
		return nil, fmt.Errorf("database error finding API keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKeyData
	for rows.Next() {
		var keyData APIKeyData
		if err := rows.Scan(&keyData.UserID, &keyData.DatabaseID, &keyData.HashedKey); err != nil {
			customLog.Warnf("Storage: Error scanning API key data for prefix '%s': %v", prefix, err)
			// Return potentially partial results or an error? Let's return error.
			return nil, fmt.Errorf("failed processing API key data: %w", err)
		}
		keys = append(keys, keyData)
	}
	if err = rows.Err(); err != nil {
		customLog.Warnf("Storage: Error iterating API key results for prefix '%s': %v", prefix, err)
		return nil, fmt.Errorf("failed reading API key data: %w", err)
	}

	// Returns empty slice if no keys found for the prefix
	return keys, nil
}
