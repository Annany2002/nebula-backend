// api/models/auth_models.go
package models

import (
	"github.com/golang-jwt/jwt/v5"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

// --- Auth Request/Response Structs ---

// SignupRequest defines the structure for the signup request body
type SignupRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Username string `json:"username" binding:"required,min=6"`
	Password string `json:"password" binding:"required,min=8"`
}

// LoginRequest defines the structure for the login request body
type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse defines the structure for the login response body
type LoginResponse struct {
	Message string              `json:"message"`
	User    domain.UserMetadata `json:"user"`
	Token   string              `json:"token"`
}

// GetUser defines the structure for the get user by user_id body
type GetUser struct {
	Token string `json:"token"`
}

// --- JWT Claims ---

// CustomClaims includes standard claims and our custom userID claim for JWT
type CustomClaims struct {
	UserID string `json:"userId"`
	jwt.RegisteredClaims
}
