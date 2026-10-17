package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

// AccountSummary exposes only the authenticated account's registered resource counts.
func (h *AuthHandler) AccountSummary(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Writer.Header().Add("Vary", "Authorization")
	if c.Request.URL.RawQuery != "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Account summary accepts no query parameters", "code": "invalid_summary_query"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	summary, err := storage.GetAccountSummary(ctx, h.DB, c.GetString("userId"))
	if err != nil {
		status, code := http.StatusInternalServerError, "summary_unavailable"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, code = http.StatusRequestTimeout, "request_timeout"
		} else if errors.Is(err, storage.ErrUserNotFound) {
			status, code = http.StatusNotFound, "user_not_found"
		}
		c.AbortWithStatusJSON(status, gin.H{"error": "Account summary unavailable", "code": code})
		return
	}
	c.JSON(http.StatusOK, gin.H{"summary": summary})
}
