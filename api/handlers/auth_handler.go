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
	"github.com/Annany2002/nebula-backend/internal/email"
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage" // Import storage functions/errors
	"github.com/Annany2002/nebula-backend/internal/verification"
)

var (
	customLog = logger.NewLogger()
)

// AuthHandler holds dependencies for authentication handlers.
type AuthHandler struct {
	Verification *verification.Service
	DB           *sql.DB        // Metadata DB connection pool
	Cfg          *config.Config // Application configuration
	// Add AuthService interface later if needed
}

// NewAuthHandler creates a new AuthHandler with dependencies.
func NewAuthHandler(db *sql.DB, cfg *config.Config) *AuthHandler {
	return NewAuthHandlerWithSender(db, cfg, email.NewSMTP(&cfg.Email.SMTP))
}

// NewAuthHandlerWithSender shares account verification with an injectable delivery boundary.
func NewAuthHandlerWithSender(db *sql.DB, cfg *config.Config, sender email.Sender) *AuthHandler {
	return &AuthHandler{
		Verification: verification.New(db, &cfg.Email, sender),
		DB:           db,
		Cfg:          cfg,
	}
}

// Signup handles user registration requests.
func (h *AuthHandler) Signup(c *gin.Context) {
	var req models.SignupRequest // Use DTO from api/models
	uuid := uuid.New().String()

	if !decodeAccountBody(c, &req, false) {
		return
	}

	if !verification.ValidateEmail(req.Email) {
		c.JSON(400, gin.H{"error": "Invalid email address", "code": "invalid_email"})
		return
	}
	if len(req.Password) > 72 {
		c.JSON(400, gin.H{"error": "Password must be at most 72 UTF-8 bytes", "code": "invalid_password"})
		return
	}
	// Hash the password using the internal auth function
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		customLog.Warnf("Failed to hash password during signup for email %s: %v", req.Email, err)
		_ = c.Error(err) // Attach internal error
		return
	}

	if h.Cfg.Email.Enabled {
		if err = storage.CreateUnverifiedUser(c.Request.Context(), h.DB, uuid, req.Username, req.Email, hashedPassword); err != nil {
			_ = c.Error(err)
			return
		}
		delivery := "sent"
		if err = h.Verification.Send(c.Request.Context(), uuid, req.Email, false); err != nil {
			delivery = "unconfirmed"
			customLog.Warn("Signup verification delivery was not confirmed")
		}
		c.JSON(http.StatusCreated, gin.H{"user_id": uuid, "message": "Account created. Verify your email before signing in.", "verification_required": true, "verification_delivery": delivery})
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

	if !decodeAccountBody(c, &req, false) {
		return // Let middleware handle
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

	access, err := storage.GetEmailVerificationStatus(c.Request.Context(), h.DB, user.UserId)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if access.Suspended {
		c.JSON(http.StatusForbidden, gin.H{"error": "This account is suspended", "code": "account_suspended"})
		return
	}
	if access.Required && !access.Verified {
		c.JSON(403, gin.H{"error": "Verify your email before signing in", "code": "email_verification_required"})
		return
	}
	// ... (generate JWT and return success) ...
	tokenString, err := auth.GenerateSessionJWT(user.UserId, h.Cfg.JWTSecret, h.Cfg.JWTExpiration, access.SessionVersion)
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

// GetCurrentUser returns the profile of the currently authenticated user.
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userId := c.MustGet("userId").(string)

	user, err := storage.FindUserByUserId(c.Request.Context(), h.DB, userId)
	if err != nil {
		customLog.Warnf("Failed to get current user profile for userId %s: %v", userId, err)
		_ = c.Error(err)
		return
	}

	// Return user profile without password hash
	c.JSON(http.StatusOK, models.UserProfileResponse{
		EmailVerified:        user.EmailVerified,
		VerificationRequired: user.VerificationRequired,
		PendingEmail:         user.PendingEmail,
		UserId:               user.UserId,
		Username:             user.Username,
		Email:                user.Email,
		CreatedAt:            user.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// UpdateCurrentUser updates the profile of the currently authenticated user.
func (h *AuthHandler) UpdateCurrentUser(c *gin.Context) {
	userId := c.MustGet("userId").(string)

	var req models.UpdateProfileRequest
	if !decodeAccountBody(c, &req, false) {
		return
	}

	// Check if there's anything to update
	if req.Username == "" && req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update. Provide 'username' or 'email'."})
		return
	}

	if req.Email != "" && !verification.ValidateEmail(req.Email) {
		c.JSON(400, gin.H{"error": "Invalid email address", "code": "invalid_email"})
		return
	}
	if req.Email != "" && h.requestEmailChange(c, userId, req) {
		return
	}
	// Update user profile
	err := storage.UpdateUser(c.Request.Context(), h.DB, userId, req.Username, req.Email)
	if err != nil {
		customLog.Warnf("Failed to update user profile for userId %s: %v", userId, err)
		_ = c.Error(err)
		return
	}

	// Fetch updated user to return
	updatedUser, err := storage.FindUserByUserId(c.Request.Context(), h.DB, userId)
	if err != nil {
		customLog.Warnf("Failed to fetch updated user profile for userId %s: %v", userId, err)
		_ = c.Error(err)
		return
	}

	customLog.Printf("Successfully updated profile for userId %s", userId)
	c.JSON(http.StatusOK, gin.H{
		"message": "Profile updated successfully",
		"user": models.UserProfileResponse{
			EmailVerified:        updatedUser.EmailVerified,
			VerificationRequired: updatedUser.VerificationRequired,
			PendingEmail:         updatedUser.PendingEmail,
			UserId:               updatedUser.UserId,
			Username:             updatedUser.Username,
			Email:                updatedUser.Email,
			CreatedAt:            updatedUser.CreatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}
