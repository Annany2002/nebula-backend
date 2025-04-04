// internal/storage/metadata_repo.go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/mattn/go-sqlite3"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

// Specific errors for metadata operations
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailExists        = errors.New("email already exists")
	ErrDatabaseExists     = errors.New("database name already exists for this user")
	ErrDatabaseNotFound   = errors.New("database not found or not registered for this user")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// --- User Operations ---

// CreateUser inserts a new user into the metadata database.
func CreateUser(ctx context.Context, db *sql.DB, email string, passwordHash string) (int64, error) {
	sqlStatement := `INSERT INTO users (email, password_hash) VALUES (?, ?)`
	result, err := db.ExecContext(ctx, sqlStatement, email, passwordHash)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			if strings.Contains(sqliteErr.Error(), "users.email") {
				return 0, ErrEmailExists
			}
		}
		log.Printf("Storage: Failed to insert user %s: %v", email, err)
		return 0, fmt.Errorf("database error during user creation: %w", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		log.Printf("Storage: Failed to get last insert ID for user %s: %v", email, err)
		return 0, fmt.Errorf("failed to retrieve user ID after creation: %w", err)
	}
	return userID, nil
}

// FindUserByEmail retrieves a user by their email address.
func FindUserByEmail(ctx context.Context, db *sql.DB, email string) (*domain.User, error) {
	sqlStatement := `SELECT id, email, password_hash, created_at FROM users WHERE email = ? LIMIT 1`
	row := db.QueryRowContext(ctx, sqlStatement, email)
	var user domain.User
	err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		log.Printf("Storage: Failed to find user by email %s: %v", email, err)
		return nil, fmt.Errorf("database error finding user: %w", err)
	}
	return &user, nil
}

// --- Database Registration Operations ---

// RegisterDatabase inserts a new database registration record.
func RegisterDatabase(ctx context.Context, db *sql.DB, userID int64, dbName string, filePath string) error {
	sqlStatement := `INSERT INTO databases (user_id, db_name, file_path) VALUES (?, ?, ?)`
	_, err := db.ExecContext(ctx, sqlStatement, userID, dbName, filePath)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			// Could be UNIQUE(user_id, db_name) or UNIQUE(file_path)
			log.Printf("Storage: Constraint violation registering DB '%s' for user %d: %v", dbName, userID, err)
			return ErrDatabaseExists // Assume name conflict for user
		}
		log.Printf("Storage: Failed to insert database record for UserID %d, DBName '%s': %v", userID, dbName, err)
		return fmt.Errorf("database error registering database: %w", err)
	}
	return nil
}

// FindDatabasePath retrieves the file path for a given user and database name.
func FindDatabasePath(ctx context.Context, db *sql.DB, userID int64, dbName string) (string, error) {
	var dbFilePath string
	lookupSQL := `SELECT file_path FROM databases WHERE user_id = ? AND db_name = ? LIMIT 1`
	err := db.QueryRowContext(ctx, lookupSQL, userID, dbName).Scan(&dbFilePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrDatabaseNotFound
		}
		log.Printf("Storage: Error looking up database path for UserID %d, DBName '%s': %v", userID, dbName, err)
		return "", fmt.Errorf("database error finding database path: %w", err)
	}
	return dbFilePath, nil
}

// ListUserDatabases retrieves a list of database names registered by a specific user.
func ListUserDatabases(ctx context.Context, db *sql.DB, userID int64) ([]string, error) {
	query := `SELECT db_name FROM databases WHERE user_id = ? ORDER BY db_name;`
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		log.Printf("Storage: Error listing databases for UserID %d: %v", userID, err)
		return nil, fmt.Errorf("database error listing databases: %w", err)
	}
	defer rows.Close()

	var dbNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Storage: Error scanning database name for UserID %d: %v", userID, err)
			return nil, fmt.Errorf("failed processing database list: %w", err)
		}
		dbNames = append(dbNames, name)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Storage: Error iterating database list for UserID %d: %v", userID, err)
		return nil, fmt.Errorf("failed reading database list: %w", err)
	}

	// Return empty slice if user has no databases, not an error
	if dbNames == nil {
		dbNames = make([]string, 0)
	}
	return dbNames, nil
}

// --- *** NEW: Delete Database Registration *** ---

// DeleteDatabaseRegistration removes the database entry from the metadata table.
// It returns ErrDatabaseNotFound if no matching entry was found.
func DeleteDatabaseRegistration(ctx context.Context, db *sql.DB, userID int64, dbName string) error {
	deleteSQL := `DELETE FROM databases WHERE user_id = ? AND db_name = ?;`
	result, err := db.ExecContext(ctx, deleteSQL, userID, dbName)
	if err != nil {
		// Likely a connection or syntax issue, not "not found"
		log.Printf("Storage: Error executing delete registration for UserID %d, DB '%s': %v", userID, dbName, err)
		return fmt.Errorf("database error deleting registration: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Storage: Error getting RowsAffected for delete registration UserID %d, DB '%s': %v", userID, dbName, err)
		return fmt.Errorf("failed confirming registration deletion: %w", err)
	}

	if rowsAffected == 0 {
		// No rows matched the user_id and db_name combination
		return ErrDatabaseNotFound
	}

	return nil // Success
}

// --- *** END NEW *** ---
