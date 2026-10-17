package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

// Suspend blocks account access without deleting tenant data or API credentials.
func (h *AdminHandler) Suspend(c *gin.Context) { h.setAccountStatus(c, true) }

// Activate restores account access; previous JWT sessions remain revoked.
func (h *AdminHandler) Activate(c *gin.Context) { h.setAccountStatus(c, false) }

func (h *AdminHandler) setAccountStatus(c *gin.Context, suspended bool) {
	if c.Request.URL.RawQuery != "" {
		adminFailure(c, storage.ErrInvalidAdminQuery)
		return
	}
	if !emptyAdminBody(c) {
		return
	}
	user, err := storage.SetAccountSuspended(c.Request.Context(), h.db, c.GetString("userId"), c.Param("user_id"), c.GetInt("sessionVersion"), suspended)
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func emptyAdminBody(c *gin.Context) bool {
	controller := http.NewResponseController(c.Writer)
	if deadline, ok := c.Request.Context().Deadline(); ok {
		_ = controller.SetReadDeadline(deadline)
		defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		status, code := http.StatusBadRequest, "invalid_admin_request"
		var oversized *http.MaxBytesError
		var timeout interface{ Timeout() bool }
		if errors.As(err, &oversized) {
			status, code = http.StatusRequestEntityTooLarge, "request_too_large"
		} else if c.Request.Context().Err() != nil || (errors.As(err, &timeout) && timeout.Timeout()) {
			status, code = http.StatusRequestTimeout, "request_timeout"
		}
		c.Header("Connection", "close")
		c.AbortWithStatusJSON(status, gin.H{"error": "Invalid administrator request", "code": code})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "Use application/json for an empty object body", "code": "unsupported_media_type"})
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil || len(object) != 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Send no body or an empty JSON object", "code": "invalid_admin_request"})
		return false
	}
	return true
}
