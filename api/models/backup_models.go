package models

import "github.com/Annany2002/nebula-backend/internal/domain"

// CreateBackupRequest uses a caller-generated UUID for retry-safe creation.
type CreateBackupRequest struct {
	BackupID string `json:"backup_id"`
}

// BackupResponse wraps public backup metadata.
type BackupResponse struct {
	Backup domain.DatabaseBackup `json:"backup"`
}

// RestoreBackupRequest always names a new destination database.
type RestoreBackupRequest struct {
	DBName string `json:"db_name"`
}

// RestoreBackupResponse acknowledges the published destination, not its future size.
type RestoreBackupResponse struct {
	Message   string `json:"message"`
	BackupID  string `json:"backup_id"`
	DBName    string `json:"db_name"`
	SizeBytes int64  `json:"size_bytes"`
}
