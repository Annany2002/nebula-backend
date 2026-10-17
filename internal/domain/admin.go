package domain

import "time"

// AccountStatus describes access independently of administrator membership.
type AccountStatus string

const (
	// AccountActive permits access subject to verification and credential checks.
	AccountActive AccountStatus = "active"
	// AccountSuspended blocks login, JWTs and database API keys.
	AccountSuspended AccountStatus = "suspended"
)

// AdminUser is an explicit projection that never loads account credentials.
type AdminUser struct {
	UserID               string        `json:"user_id"`
	Username             string        `json:"username"`
	Email                string        `json:"email"`
	CreatedAt            time.Time     `json:"created_at"`
	EmailVerified        bool          `json:"email_verified"`
	VerificationRequired bool          `json:"verification_required"`
	Role                 string        `json:"role"`
	Status               AccountStatus `json:"status"`
	DatabaseCount        int64         `json:"database_count"`
}

// PlatformStats distinguishes complete table totals from unavailable tenant schemas.
type PlatformStats struct {
	TotalUsers           int64  `json:"total_users"`
	TotalDatabases       int64  `json:"total_databases"`
	TotalTables          *int64 `json:"total_tables"`
	DatabasesCounted     int64  `json:"databases_counted"`
	DatabasesUnavailable int64  `json:"databases_unavailable"`
}
