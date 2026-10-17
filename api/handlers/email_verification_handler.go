package handlers

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
	"github.com/Annany2002/nebula-backend/internal/verification"
)

// VerifyEmail accepts a token in a POST body so it never enters access logs or query telemetry.
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	var request struct {
		Token string `json:"token"`
	}
	if !decodeAccountJSONBody(c, &request) {
		return
	}
	err := h.Verification.Verify(c.Request.Context(), request.Token)
	if err != nil {
		status, code := 500, "verification_unavailable"
		if errors.Is(err, storage.ErrVerificationToken) {
			status, code = 400, "invalid_verification_token"
		}
		if errors.Is(err, storage.ErrEmailExists) {
			status, code = 409, "email_conflict"
		}
		c.JSON(status, gin.H{"error": "Email verification could not be completed", "code": code})
		return
	}
	c.JSON(200, gin.H{"message": "Email verified successfully. Sign in to continue."})
}

// ResendVerification acknowledges eligible delivery without exposing account existence.
func (h *AuthHandler) ResendVerification(c *gin.Context) {
	var request struct {
		Email string `json:"email"`
	}
	if !decodeAccountJSONBody(c, &request) {
		return
	}
	if !verification.ValidateEmail(request.Email) {
		c.JSON(400, gin.H{"error": "Invalid email address", "code": "invalid_email"})
		return
	}
	if !h.Cfg.Email.Enabled {
		c.JSON(503, gin.H{"error": "Email verification delivery is not configured", "code": "verification_disabled"})
		return
	}
	if !h.Verification.QueueResend(request.Email) {
		c.JSON(503, gin.H{"error": "Verification delivery is busy. Try again shortly.", "code": "verification_busy"})
		return
	}
	c.JSON(202, gin.H{"message": "If this address is eligible, a verification email will be sent."})
}

// requestEmailChange requires the password and leaves the current address active until confirmed.
func (h *AuthHandler) requestEmailChange(c *gin.Context, id string, request models.UpdateProfileRequest) bool {
	user, err := storage.FindUserByUserId(c.Request.Context(), h.DB, id)
	if err != nil {
		_ = c.Error(err)
		return true
	}
	if user.Email == request.Email {
		return false
	}
	if !h.Cfg.Email.Enabled {
		if !user.VerificationRequired {
			return false
		}
		c.JSON(503, gin.H{"error": "Email verification delivery is not configured", "code": "verification_disabled"})
		return true
	}
	if !auth.CheckPasswordHash(request.CurrentPassword, user.PasswordHash) {
		c.JSON(401, gin.H{"error": "Confirm your current password to change email", "code": "reauthentication_required"})
		return true
	}
	if request.Username != "" {
		c.JSON(400, gin.H{"error": "Change email separately from your username", "code": "invalid_email_change"})
		return true
	}
	err = h.Verification.RequestChange(c.Request.Context(), id, request.Email, user.SessionVersion)
	if err != nil {
		status, code := 503, "verification_delivery_unconfirmed"
		if errors.Is(err, storage.ErrVerificationRateLimit) {
			status, code = 429, "verification_rate_limited"
			c.Header("Retry-After", "60")
			var limit *storage.VerificationRateLimitError
			if errors.As(err, &limit) {
				c.Header("Retry-After", strconv.FormatInt(limit.RetryAfterSeconds, 10))
			}
		}
		c.JSON(status, gin.H{"error": "Email change was not confirmed. Your current email remains active.", "code": code})
		return true
	}
	c.JSON(202, gin.H{"message": "Confirm the link sent to your new address. Your current email remains active.", "pending_email": request.Email})
	return true
}
