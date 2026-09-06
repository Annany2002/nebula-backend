// internal/storage/metadata_repo.go
package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
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
	ErrConflict           = errors.New("cannot generate more than one api key for a database")
	ErrAPIKeyGeneration   = errors.New("failed to generate api key components")
	ErrAPIKeyNotFound     = errors.New("api key not found")
)

const authKeyPrefixMeta = "neb_" // nolint:gosec // API key prefix identifier, not a secret
const apiKeySecretLength = 32    // Length of the random secret part in bytes

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

// UpdateUser updates user profile fields (username and/or email).
func UpdateUser(ctx context.Context, db *sql.DB, userId, username, email string) error {
	// Build dynamic UPDATE query based on provided fields
	setClauses := []string{}
	args := []interface{}{}

	if username != "" {
		setClauses = append(setClauses, "username = ?")
		args = append(args, username)
	}
	if email != "" {
		setClauses = append(setClauses, "email = ?")
		args = append(args, email)
	}

	if len(setClauses) == 0 {
		return nil // Nothing to update
	}

	args = append(args, userId)
	// nolint:gosec // setClauses only contains hardcoded column names ("username = ?" or "email = ?")
	sqlStatement := fmt.Sprintf("UPDATE users SET %s WHERE user_id = ?", strings.Join(setClauses, ", "))

	result, err := db.ExecContext(ctx, sqlStatement, args...)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			if strings.Contains(sqliteErr.Error(), "users.email") {
				return ErrEmailExists
			}
		}
		customLog.Warnf("Storage: Failed to update user %s: %v", userId, err)
		return fmt.Errorf("database error during user update: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to confirm user update: %w", err)
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
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

		if err := userSingleDb.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_nebula_%';").Scan(&singleDb.Tables); err != nil {
			customLog.Warnf("Error counting tables in %s: %v\n", singleDb.FilePath, err)
			userSingleDb.Close()
			continue
		}
		userSingleDb.Close()

		apiKey, err := FindAPIKeyByDatabaseId(ctx, db, singleDb.DatabaseID)
		if err != nil {
			customLog.Warnf("Error in retrieving api keys for %s: %v", singleDb.DBName, err)
		}

		singleDb.APIKey = apiKey
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
func FindDatabaseIDByNameAndUser(ctx context.Context, db *sql.DB, userId, dbName string) (int64, error) {
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

// StoreAPIKey generates and stores a new API key scoped to a specific user and database.
// It returns the *full, unhashed* key (prefix + secret) ONCE upon successful creation.
func StoreAPIKey(ctx context.Context, db *sql.DB, userId string, databaseId int64) (string, error) {
	// Generate cryptographically secure random bytes for the secret
	randomBytes := make([]byte, apiKeySecretLength)
	_, err := rand.Read(randomBytes)
	if err != nil {
		customLog.Warnf("Storage: Failed to generate random bytes for API key: %v", err)
		return "", ErrAPIKeyGeneration
	}

	// Encode random bytes to a URL-safe base64 string for the secret part
	secret := base64.RawURLEncoding.EncodeToString(randomBytes)

	key := authKeyPrefixMeta + secret
	// Store the prefix, HASHED secret, and other details in the DB
	insertSQL := `INSERT INTO api_keys (api_owner_id, api_database_id, key) VALUES (?, ?, ?);`
	_, err = db.ExecContext(ctx, insertSQL, userId, databaseId, key)
	if err != nil {
		// Handle potential constraint violations (e.g., UNIQUE on hashed_key, though collisions are extremely unlikely)
		customLog.Warnf("Storage: Failed to store API key for UserID %v, DBID %d: %v", userId, databaseId, err)
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
			return "", ErrConflict
		}
		return "", fmt.Errorf("database error storing API key: %w", err)
	}

	return key, nil
}

// FindAPIKeyByDatabaseId retrieves potential key for a particular user
func FindAPIKeyByDatabaseId(ctx context.Context, db *sql.DB, databaseId int64) (string, error) {
	query := `SELECT key FROM api_keys WHERE api_database_id = ?;`
	rows, err := db.QueryContext(ctx, query, databaseId)
	if err != nil {
		customLog.Warnf("Storage: Error querying API keys by database_id  '%d': %v", databaseId, err)
		// Don't return specific errors like Not Found here, let middleware handle empty results
		return "", fmt.Errorf("database error finding API keys: %w", err)
	}
	defer rows.Close()

	var key string
	for rows.Next() {
		if err := rows.Scan(&key); err != nil {
			customLog.Warnf("Storage: Error scanning API key data with database_id '%d': %v", databaseId, err)
			// Return potentially partial results or an error? Let's return error.
			return "", fmt.Errorf("failed processing API key data: %w", err)
		}

	}
	if err = rows.Err(); err != nil {
		customLog.Warnf("Storage: Error iterating API key results wuth database_id '%d': %v", databaseId, err)
		return "", fmt.Errorf("failed reading API key data: %w", err)
	}

	// Returns empty slice if no keys found for the prefix
	return key, nil
}

// DeleteAPIKey deletes the api key from the database
func DeleteAPIKey(ctx context.Context, db *sql.DB, key string) error {
	deleteSQL := `DELETE FROM api_keys WHERE key = ?`

	result, err := db.ExecContext(ctx, deleteSQL, key)
	if err != nil {

		customLog.Warnf("Storage: Error executing delete api key : %s, DB ", key)
		return fmt.Errorf("database error deleting registration: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		customLog.Warnf("Storage: Error getting RowsAffected for delete api key : %s", key)
		return fmt.Errorf("failed confirming registration deletion: %w", err)
	}

	if rowsAffected == 0 {
		// No rows matched
		return ErrAPIKeyNotFound
	}

	return nil // Success
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// GetDatabaseDetails retrieves detailed database metadata including disk size and total records.
func GetDatabaseDetails(ctx context.Context, metaDB *sql.DB, userId, dbName string) (*domain.DatabaseDetailMetadata, error) {
	query := `SELECT database_id, owner_id, db_name, file_path, created_at FROM databases WHERE owner_id = ? AND db_name = ? LIMIT 1;`
	var detail domain.DatabaseDetailMetadata
	err := metaDB.QueryRowContext(ctx, query, userId, dbName).Scan(
		&detail.DatabaseID,
		&detail.UserID,
		&detail.DBName,
		&detail.FilePath,
		&detail.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDatabaseNotFound
		}
		return nil, fmt.Errorf("database error retrieving details: %w", err)
	}

	// File size on disk
	if fi, err := os.Stat(detail.FilePath); err == nil {
		detail.SizeBytes = fi.Size()
		detail.SizeDisplay = formatBytes(fi.Size())
	} else {
		detail.SizeDisplay = "0 B"
	}

	// Connect to user database to count tables and total records
	userDB, err := ConnectUserDB(ctx, detail.FilePath)
	if err == nil {
		defer userDB.Close()

		// Count tables
		_ = userDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_nebula_%';").Scan(&detail.Tables)

		// Sum records across all tables
		rows, err := userDB.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_nebula_%';")
		if err == nil {
			defer rows.Close()
			var totalRecords int64
			for rows.Next() {
				var tblName string
				if err := rows.Scan(&tblName); err == nil {
					var count int64
					_ = userDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s;", tblName)).Scan(&count)
					totalRecords += count
				}
			}
			detail.TotalRecords = totalRecords
		}
	}

	// Active API Key
	apiKey, _ := FindAPIKeyByDatabaseId(ctx, metaDB, detail.DatabaseID)
	detail.APIKey = apiKey

	return &detail, nil
}

// RecordTelemetry inserts an API request telemetry record.
func RecordTelemetry(ctx context.Context, metaDB *sql.DB, dbName, endpoint, method string, statusCode int, latencyMs int64) error {
	query := `INSERT INTO database_telemetry (database_name, endpoint, method, status_code, latency_ms) VALUES (?, ?, ?, ?, ?);`
	_, err := metaDB.ExecContext(ctx, query, dbName, endpoint, method, statusCode, latencyMs)
	return err
}

// GetDatabaseAnalytics aggregates real telemetry and runs SQLite schema advisor checks.
func GetDatabaseAnalytics(ctx context.Context, metaDB *sql.DB, userId, dbName string) (*domain.DatabaseAnalytics, error) {
	detail, err := GetDatabaseDetails(ctx, metaDB, userId, dbName)
	if err != nil {
		return nil, err
	}

	analytics := &domain.DatabaseAnalytics{
		Timeframe: "24h",
		Services:  make([]domain.ServiceMetrics, 0),
		Advisor:   make([]domain.AdvisorIssue, 0),
	}

	// Query total telemetry in the last 24h
	var totalReqs, successReqs int64
	err = metaDB.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0)
		FROM database_telemetry 
		WHERE database_name = ? AND created_at >= datetime('now', '-24 hours');
	`, dbName).Scan(&totalReqs, &successReqs)
	if err == nil {
		analytics.TotalRequests = totalReqs
		if totalReqs > 0 {
			analytics.SuccessRate = float64(successReqs) / float64(totalReqs) * 100.0
		} else {
			analytics.SuccessRate = 100.0
		}
	}

	// Service categories: SQL Engine, Records API, Tables API, Auth & Keys
	categories := []struct {
		name    string
		pattern string
	}{
		{"SQL Engine", "%/sql%"},
		{"Records API", "%/records%"},
		{"Tables API", "%/tables%"},
		{"Auth & Keys", "%/apikey%"},
	}

	for _, cat := range categories {
		var reqs, warns, errs int64
		_ = metaDB.QueryRowContext(ctx, `
			SELECT 
				COUNT(*),
				COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code < 500 THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0)
			FROM database_telemetry 
			WHERE database_name = ? AND endpoint LIKE ? AND created_at >= datetime('now', '-24 hours');
		`, dbName, cat.pattern).Scan(&reqs, &warns, &errs)

		history := make([]domain.ServiceMetricBucket, 0)
		rows, qErr := metaDB.QueryContext(ctx, `
			SELECT 
				strftime('%H:00', created_at) as hour_bucket,
				COUNT(*),
				COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code < 500 THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0)
			FROM database_telemetry
			WHERE database_name = ? AND endpoint LIKE ? AND created_at >= datetime('now', '-24 hours')
			GROUP BY hour_bucket
			ORDER BY hour_bucket ASC
			LIMIT 12;
		`, dbName, cat.pattern)
		if qErr == nil {
			for rows.Next() {
				var b domain.ServiceMetricBucket
				if scanErr := rows.Scan(&b.Timestamp, &b.Requests, &b.Warnings, &b.Errors); scanErr == nil {
					history = append(history, b)
				}
			}
			rows.Close()
		}

		analytics.Services = append(analytics.Services, domain.ServiceMetrics{
			Name:     cat.name,
			Requests: reqs,
			Warnings: warns,
			Errors:   errs,
			History:  history,
		})
	}

	// Run Real Schema Advisor on SQLite User DB
	userDB, err := ConnectUserDB(ctx, detail.FilePath)
	if err == nil {
		defer userDB.Close()

		// A. Check Journal Mode
		var journalMode string
		if jErr := userDB.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); jErr == nil {
			if !strings.EqualFold(journalMode, "wal") {
				analytics.Advisor = append(analytics.Advisor, domain.AdvisorIssue{
					ID:          "journal-mode-wal",
					Category:    "PERFORMANCE",
					Severity:    "WARNING",
					Title:       fmt.Sprintf("Database running in '%s' journal mode", strings.ToUpper(journalMode)),
					Description: "WAL (Write-Ahead Logging) mode is recommended for SQLite to enable concurrent reads without blocking writes.",
					Suggestion:  "Run 'PRAGMA journal_mode=WAL;' in SQL Editor.",
				})
			}
		}

		// B. Check Foreign Keys enforcement
		var foreignKeysEnabled int
		if fkErr := userDB.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&foreignKeysEnabled); fkErr == nil {
			if foreignKeysEnabled == 0 {
				analytics.Advisor = append(analytics.Advisor, domain.AdvisorIssue{
					ID:          "foreign-keys-disabled",
					Category:    "SECURITY",
					Severity:    "INFO",
					Title:       "Foreign Key constraints not enforced by default",
					Description: "SQLite defaults to PRAGMA foreign_keys = OFF, which may allow orphan rows across relations.",
					Suggestion:  "Enable foreign key enforcement with 'PRAGMA foreign_keys = ON;'.",
				})
			}
		}

		// C. Check Integrity
		var integrityCheck string
		if iErr := userDB.QueryRowContext(ctx, "PRAGMA integrity_check(1);").Scan(&integrityCheck); iErr == nil {
			if !strings.EqualFold(integrityCheck, "ok") {
				analytics.Advisor = append(analytics.Advisor, domain.AdvisorIssue{
					ID:          "integrity-failure",
					Category:    "SECURITY",
					Severity:    "CRITICAL",
					Title:       "Database integrity anomaly detected",
					Description: fmt.Sprintf("SQLite PRAGMA integrity_check returned: %s", integrityCheck),
					Suggestion:  "Review SQLite file corruption or restore from a backup.",
				})
			}
		}

		// D. Check Tables for missing Primary Keys and Empty status
		tblRows, tErr := userDB.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_nebula_%';")
		if tErr == nil {
			for tblRows.Next() {
				var tblName string
				if sErr := tblRows.Scan(&tblName); sErr == nil {
					infoRows, pErr := userDB.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", tblName))
					hasPK := false
					if pErr == nil {
						for infoRows.Next() {
							var cid, pk, notnull int
							var colName, colType string
							var dfltVal any
							if scanErr := infoRows.Scan(&cid, &colName, &colType, &notnull, &dfltVal, &pk); scanErr == nil {
								if pk > 0 {
									hasPK = true
								}
							}
						}
						infoRows.Close()
					}

					if !hasPK {
						analytics.Advisor = append(analytics.Advisor, domain.AdvisorIssue{
							ID:          fmt.Sprintf("missing-pk-%s", tblName),
							Category:    "SECURITY",
							Severity:    "CRITICAL",
							Title:       fmt.Sprintf("Table '%s' has no Primary Key", tblName),
							Description: fmt.Sprintf("Records in '%s' cannot be uniquely identified, impacting reliable row mutations.", tblName),
							TableName:   tblName,
							Suggestion:  fmt.Sprintf("Add a PRIMARY KEY column to table '%s'.", tblName),
						})
					}

					var rowCount int64
					if countErr := userDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s;", tblName)).Scan(&rowCount); countErr == nil {
						if rowCount == 0 {
							analytics.Advisor = append(analytics.Advisor, domain.AdvisorIssue{
								ID:          fmt.Sprintf("empty-table-%s", tblName),
								Category:    "SCHEMA",
								Severity:    "INFO",
								Title:       fmt.Sprintf("Table '%s' contains 0 records", tblName),
								Description: fmt.Sprintf("Schema is configured for '%s', but no rows have been inserted.", tblName),
								TableName:   tblName,
								Suggestion:  "Insert records using Table Editor or SQL Runner.",
							})
						}
					}
				}
			}
			tblRows.Close()
		}
	}

	return analytics, nil
}
