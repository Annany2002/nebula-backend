package handlers

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/storage"
	"github.com/Annany2002/nebula-backend/internal/verification"
)

// ForgotPassword acknowledges a bounded asynchronous request without disclosing account state.
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
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
	if !h.passwordResetEnabled(c) {
		return
	}
	if !h.Recovery.QueueRequest(request.Email) {
		c.JSON(503, gin.H{"error": "Password recovery is busy. Try again shortly.", "code": "password_reset_busy"})
		return
	}
	c.JSON(202, gin.H{"message": "If this address is eligible, a password reset email will be sent."})
}

func (h *AuthHandler) passwordResetEnabled(c *gin.Context) bool {
	if !h.Cfg.Email.PasswordResetEnabled {
		c.JSON(503, gin.H{"error": "Password recovery is not configured", "code": "password_reset_disabled"})
		return false
	}
	return true
}

// ResetPassword consumes a token only after a new password is explicitly submitted.
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var request struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeAccountJSONBody(c, &request) {
		return
	}
	if !h.passwordResetEnabled(c) {
		return
	}
	if utf8.RuneCountInString(request.Password) < 8 || len(request.Password) > 72 {
		c.JSON(400, gin.H{"error": "Password must contain at least 8 characters and at most 72 UTF-8 bytes", "code": "invalid_password"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := h.Recovery.Reset(ctx, request.Token, request.Password); err != nil {
		status, code := 503, "password_reset_unavailable"
		if errors.Is(err, storage.ErrPasswordResetToken) {
			status, code = 400, "invalid_password_reset_token"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, code = 408, "request_timeout"
		}
		c.JSON(status, gin.H{"error": "Password reset could not be completed", "code": code})
		return
	}
	c.JSON(200, gin.H{"message": "Password reset successfully. Sign in with your new password."})
}
