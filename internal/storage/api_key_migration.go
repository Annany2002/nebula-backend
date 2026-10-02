package storage

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
)

func hashAPIKey(key string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(key)))
}

func apiKeyDisplayPrefix(key string) string {
	length := 12
	if strings.HasPrefix(key, "neb_live_") {
		length = 17
	}
	if len(key) < length {
		length = len(key)
	}
	return key[:length] + "..."
}

// Migrate in one transaction so existing credentials remain valid after restart.
func migrateAPIKeys(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Rollback is harmless after a successful commit.
	rows, err := tx.Query("PRAGMA table_info(api_keys)")
	if err != nil {
		return err
	}
	hasPrefix := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "key_prefix" {
			hasPrefix = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasPrefix {
		if _, err := tx.Exec("ALTER TABLE api_keys ADD COLUMN key_prefix TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	rows, err = tx.Query("SELECT api_key_id,key FROM api_keys WHERE key NOT LIKE 'sha256:%'")
	if err != nil {
		return err
	}
	type legacyKey struct {
		id  int64
		key string
	}
	var legacy []legacyKey
	for rows.Next() {
		var entry legacyKey
		if err := rows.Scan(&entry.id, &entry.key); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, entry := range legacy {
		if _, err := tx.Exec("UPDATE api_keys SET key = ?,key_prefix = ? WHERE api_key_id = ?", hashAPIKey(entry.key), apiKeyDisplayPrefix(entry.key), entry.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
