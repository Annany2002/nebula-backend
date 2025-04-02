// api/models/auth_models.go
package models

import "github.com/golang-jwt/jwt/v5"

// --- Auth Request/Response Structs ---

// SignupRequest defines the structure for the signup request body
type SignupRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

// LoginRequest defines the structure for the login request body
type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse defines the structure for the login response body
type LoginResponse struct {
	Message string `json:"message"`
	Token   string `json:"token"`
}

// --- JWT Claims ---

// CustomClaims includes standard claims and our custom userID claim for JWT
type CustomClaims struct {
	UserID int64 `json:"userID"`
	jwt.RegisteredClaims
}
