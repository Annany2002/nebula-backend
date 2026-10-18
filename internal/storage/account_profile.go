package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

func ensureAccountProfileTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS account_profiles (
 user_id TEXT PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
 avatar_url TEXT NOT NULL DEFAULT '',
 avatar_source TEXT NOT NULL DEFAULT '' CHECK(avatar_source IN ('','github','upload','none')),
 avatar_id TEXT UNIQUE,
 avatar_content BLOB,
 onboarding_completed INTEGER NOT NULL DEFAULT 0 CHECK(onboarding_completed IN (0,1)),
 CHECK(avatar_content IS NULL OR length(avatar_content)<=131072)
 )`)
	return err
}

// SaveAvatar replaces the owner's image atomically; old public asset IDs stop resolving.
func SaveAvatar(ctx context.Context, db *sql.DB, userID string, content []byte) error {
	id := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO account_profiles(user_id,avatar_url,avatar_source,avatar_id,avatar_content,onboarding_completed)
 VALUES(?,?,'upload',?,?,1) ON CONFLICT(user_id) DO UPDATE SET avatar_url=excluded.avatar_url,avatar_source='upload',avatar_id=excluded.avatar_id,avatar_content=excluded.avatar_content,onboarding_completed=1
 WHERE account_profiles.avatar_source!='upload' OR account_profiles.avatar_content IS NOT excluded.avatar_content`, userID, "/avatars/"+id, id, content)
	return err
}

// RemoveAvatar records an explicit opt-out so later GitHub logins cannot undo that choice.
func RemoveAvatar(ctx context.Context, db *sql.DB, userID string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO account_profiles(user_id,avatar_source,onboarding_completed) VALUES(?,'none',1)
 ON CONFLICT(user_id) DO UPDATE SET avatar_url='',avatar_source='none',avatar_id=NULL,avatar_content=NULL,onboarding_completed=1`, userID)
	return err
}

func CompleteOnboarding(ctx context.Context, db *sql.DB, userID string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO account_profiles(user_id,onboarding_completed) VALUES(?,1)
 ON CONFLICT(user_id) DO UPDATE SET onboarding_completed=1`, userID)
	return err
}

func ReadAvatar(ctx context.Context, db *sql.DB, id string) ([]byte, error) {
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		return nil, ErrUserNotFound
	}
	var content []byte
	err := db.QueryRowContext(ctx, `SELECT avatar_content FROM account_profiles WHERE avatar_id=? AND avatar_source='upload'`, id).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return content, err
}
