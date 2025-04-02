// internal/domain/models.go
package domain

import "time"

// User defines the structure for user data in the DB
type User struct {
	ID           int64
	Email        string
	PasswordHash string // Keep unexported or handle carefully if needed elsewhere
	CreatedAt    time.Time
}

// Add other core domain models here if needed later (e.g., DatabaseMetadata)
// type DatabaseMetadata struct {
//     ID        int64
//     UserID    int64
//     DBName    string
//     FilePath  string
//     CreatedAt time.Time
// }
