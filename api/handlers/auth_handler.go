// api/handlers/auth_handler.go
package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth" // Import internal auth logic
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage" // Import storage functions/errors
)

var (
	customLog = logger.NewLogger()
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
	uuid := uuid.New().String()

	if err := c.ShouldBindJSON(&req); err != nil {
		customLog.Warnf("Signup binding error: %v", err)
		_ = c.Error(err) // Attach the binding error
		return
	}

	// Hash the password using the internal auth function
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		customLog.Warnf("Failed to hash password during signup for email %s: %v", req.Email, err)
		_ = c.Error(err) // Attach internal error
		return
	}

	// Create user using the storage function
	user_id, err := storage.CreateUser(c.Request.Context(), h.DB, uuid, req.Username, req.Email, hashedPassword)
	if err != nil {
		customLog.Warnf("Failed to create user %s: %v", req.Email, err) // Log context
		_ = c.Error(err)                                                // Attach storage error (e.g., ErrEmailExists)
		return                                                          // Let middleware handle response
	}

	customLog.Printf("Successfully registered user with email %s", req.Email)
	c.JSON(http.StatusCreated, gin.H{"user_id": user_id, "message": "User registered successfully"}) // Success response remains
}

// Login handles user login requests and issues JWT on success.
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		customLog.Warnf("Login binding error: %v", err)
		_ = c.Error(err) // Attach binding error
		return           // Let middleware handle
	}

	user, err := storage.FindUserByEmail(c.Request.Context(), h.DB, req.Email)
	if err != nil || user == nil {
		customLog.Warnf("Login failed for email %s: %v", req.Email, err)
		_ = c.Error(err) // Attach ErrUserNotFound or DB error
		return
	}

	if !auth.CheckPasswordHash(req.Password, user.PasswordHash) {
		customLog.Warnf("Login attempt failed for email %s: invalid password", user.Email)
		// *** CHANGED: Use the specific error variable ***
		_ = c.Error(storage.ErrInvalidCredentials)
		return // Let middleware handle
	}

	// ... (generate JWT and return success) ...
	tokenString, err := auth.GenerateJWT(user.UserId, h.Cfg.JWTSecret, h.Cfg.JWTExpiration)
	if err != nil {
		customLog.Warnf("Failed to generate JWT for user %s: %v", user.UserId, err)
		_ = c.Error(err) // Attach JWT generation error
		return
	}
	// ... success response ...

	c.JSON(http.StatusOK, models.LoginResponse{Message: "Logged in successfully", User: *user, Token: tokenString})
}

// Find handles find user by user_id
func (h *AuthHandler) FindUser(c *gin.Context) {
	user_id := c.Param("user_id")

	user, err := storage.FindUserByUserId(c.Request.Context(), h.DB, user_id)

	if err != nil {
		customLog.Warnf("User with user_id %s not found", user_id)
		// _ = c.Error(err)
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found", "user": nil})
		return
	}

	c.JSON(http.StatusOK, user)
}
