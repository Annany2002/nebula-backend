package domain

import "time"

// AdminUser is an explicit projection that never loads account credentials.
type AdminUser struct {
	UserID               string    `json:"user_id"`
	Username             string    `json:"username"`
	Email                string    `json:"email"`
	CreatedAt            time.Time `json:"created_at"`
	EmailVerified        bool      `json:"email_verified"`
	VerificationRequired bool      `json:"verification_required"`
	Role                 string    `json:"role"`
	DatabaseCount        int64     `json:"database_count"`
}

// PlatformStats counts registered resources without inspecting tenant data.
type PlatformStats struct {
	TotalUsers     int64 `json:"total_users"`
	TotalDatabases int64 `json:"total_databases"`
}
