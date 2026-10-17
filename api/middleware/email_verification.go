package middleware

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

// RequireAccountAccess checks suspension, verification and sessions for JWTs and API keys.
func RequireAccountAccess(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.MustGet("userId").(string)
		status, err := storage.GetEmailVerificationStatus(c.Request.Context(), db, id)
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				c.AbortWithStatusJSON(408, gin.H{"error": "Request cancelled or timed out", "code": "request_timeout"})
				return
			}
			if err == storage.ErrUserNotFound {
				code = http.StatusUnauthorized
			}
			c.AbortWithStatusJSON(code, gin.H{"error": "Account authentication is unavailable", "code": "account_unavailable"})
			return
		}
		if status.Suspended {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "This account is suspended", "code": "account_suspended"})
			return
		}
		if status.Required && !status.Verified {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Verify your email before using this account", "code": "email_verification_required"})
			return
		}
		if version, ok := c.Get("sessionVersion"); ok && version != status.SessionVersion {
			c.AbortWithStatusJSON(401, gin.H{"error": "Sign in again to continue", "code": "session_revoked"})
			return
		}
		c.Next()
	}
}

// LimitVerificationRequests uses a persistent budget for public email-sending routes.
func LimitVerificationRequests(db *sql.DB, enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		ip := c.RemoteIP()
		// The local Cloudflare connector supplies the client address; external callers cannot.
		if address := net.ParseIP(ip); address != nil && address.IsLoopback() {
			if client := net.ParseIP(c.GetHeader("CF-Connecting-IP")); client != nil {
				ip = client.String()
			}
		}
		digest := sha256.Sum256([]byte(ip))
		allowed, err := storage.AllowEmailRequest(c.Request.Context(), db, hex.EncodeToString(digest[:]), time.Now())
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "Verification requests are unavailable", "code": "verification_unavailable"})
			return
		}
		if !allowed {
			c.Header("Retry-After", "900")
			c.AbortWithStatusJSON(429, gin.H{"error": "Too many verification requests", "code": "verification_rate_limited"})
			return
		}
		c.Next()
	}
}
