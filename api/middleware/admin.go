package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

// AdminReadContext bounds authorization and queries, and prevents shared PII caching.
func AdminReadContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Writer.Header().Add("Vary", "Authorization")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequirePlatformAdmin follows JWT authentication and durable account/session checks.
func RequirePlatformAdmin(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := c.Get("userId")
		userID, valid := id.(string)
		if key, _ := c.Get("isApiKey"); key == true || !ok || !valid || userID == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Platform administrator access required", "code": "admin_required"})
			return
		}
		allowed, err := storage.IsPlatformAdmin(c.Request.Context(), db, userID)
		if err != nil {
			customLog.Warnf("Platform admin authorization failed: %v", err)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Administrator authorization unavailable", "code": "admin_unavailable"})
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Platform administrator access required", "code": "admin_required"})
			return
		}
		c.Next()
	}
}
