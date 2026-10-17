package domain

import "time"

// AccountSummary counts owned metadata; it does not inspect tenant or backup files.
type AccountSummary struct {
	DatabaseCount     int64 `json:"database_count"`
	ActiveAPIKeyCount int64 `json:"active_api_key_count"`
	ReadyBackupCount  int64 `json:"ready_backup_count"`
}

// AdminAuditEvent is a credential-free record of an actual account state transition.
type AdminAuditEvent struct {
	EventID      int64     `json:"event_id"`
	ActorUserID  string    `json:"actor_user_id"`
	TargetUserID string    `json:"target_user_id"`
	Action       string    `json:"action"`
	CreatedAt    time.Time `json:"created_at"`
}
