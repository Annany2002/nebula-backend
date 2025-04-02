// api/handlers/auth_handler.go
package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth"    // Import internal auth logic
	"github.com/Annany2002/nebula-backend/internal/storage" // Import storage functions/errors
)

// AuthHandler holds dependencies for authentication handlers.
type AuthHandler struct {
	DB  *sql.DB        // Metadata DB connection pool
	Cfg *config.Config // Application configuration
	// Add AuthService interface later if needed
}

// NewAuthHandler creates a new AuthHandler with dependencies.
func NewAuthHandler(db *sql.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		DB:  db,
		Cfg: cfg,
	}
}

// Signup handles user registration requests.
func (h *AuthHandler) Signup(c *gin.Context) {
	var req models.SignupRequest // Use DTO from api/models

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Signup binding error: %v", err)
		_ = c.Error(err) // Attach the binding error
		return
	}

	// Hash the password using the internal auth function
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		log.Printf("Failed to hash password during signup for email %s: %v", req.Email, err)
		_ = c.Error(err) // Attach internal error
		return
	}

	// Create user using the storage function
	_, err = storage.CreateUser(c.Request.Context(), h.DB, req.Email, hashedPassword)
	if err != nil {
		log.Printf("Failed to create user %s: %v", req.Email, err) // Log context
		_ = c.Error(err)                                           // Attach storage error (e.g., ErrEmailExists)
		return                                                     // Let middleware handle response
	}

	log.Printf("Successfully registered user with email %s", req.Email)
	c.JSON(http.StatusCreated, gin.H{"message": "User registered successfully"}) // Success response remains
}

// Login handles user login requests and issues JWT on success.
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest // Use DTO

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Login binding error: %v", err)
		_ = c.Error(err)
		return
	}

	// Find user by email using the storage function
	user, err := storage.FindUserByEmail(c.Request.Context(), h.DB, req.Email)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			log.Printf("Login attempt failed for email %s: user not found", req.Email)
			_ = c.Error(err)
			return
		}
		// Handle other potential database errors
		log.Printf("Database error during login for email %s: %v", req.Email, err)
		_ = c.Error(err)
		return
	}

	// Check password using the internal auth function
	if !auth.CheckPasswordHash(req.Password, user.PasswordHash) {
		log.Printf("Login attempt failed for email %s: invalid password", user.Email)
		_ = c.Error(err)
		return
	}

	// Generate JWT using the internal auth function
	tokenString, err := auth.GenerateJWT(user.ID, h.Cfg.JWTSecret, h.Cfg.JWTExpiration)
	if err != nil {
		log.Printf("Failed to generate JWT for user %d: %v", user.ID, err)
		_ = c.Error(err)
		return
	}

	log.Printf("JWT generated successfully for user ID %d", user.ID)
	c.JSON(http.StatusOK, models.LoginResponse{ // Use response DTO
		Message: "Login successful",
		Token:   tokenString,
	})
}
