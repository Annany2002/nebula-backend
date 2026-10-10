package domain

// DatabaseBackup describes an owner-managed snapshot; filesystem paths stay private.
type DatabaseBackup struct {
	BackupID  string `json:"backup_id"`
	DBName    string `json:"db_name"`
	Status    string `json:"status"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"created_at"`
}
