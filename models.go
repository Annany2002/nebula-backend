package main

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- Domain Model ---

// User defines the structure for user data in the DB
type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // Exclude password hash from JSON responses
	CreatedAt    time.Time `json:"created_at"`
}

// --- Request/DTO Structs ---

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

// --- JWT Claims ---

// CustomClaims includes standard claims and our custom userID claim
type CustomClaims struct {
	UserID int64 `json:"userID"`
	jwt.RegisteredClaims
}
