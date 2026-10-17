package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// AdminHandler exposes metadata-only reads behind the platform administrator gate.
type AdminHandler struct{ db *sql.DB }

// NewAdminHandler shares the existing metadata connection pool.
func NewAdminHandler(db *sql.DB) *AdminHandler { return &AdminHandler{db: db} }

// ListUsers returns a bounded page with no credential-bearing account fields.
func (h *AdminHandler) ListUsers(c *gin.Context) {
	limit, offset, err := adminPagination(c.Request.URL.RawQuery)
	if err != nil {
		adminFailure(c, err)
		return
	}
	users, total, err := storage.ListAdminUsers(c.Request.Context(), h.db, limit, offset)
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, models.AdminUserListResponse{Users: users, Pagination: models.AdminPagination{Total: total, Limit: limit, Offset: offset}})
}

// GetUser returns an account's safe metadata, without access to its tenant data.
func (h *AdminHandler) GetUser(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		adminFailure(c, storage.ErrInvalidAdminPage)
		return
	}
	user, err := storage.GetAdminUser(c.Request.Context(), h.db, c.Param("user_id"))
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// Stats reports registered users and databases; tenant table counts are not scanned.
func (h *AdminHandler) Stats(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		adminFailure(c, storage.ErrInvalidAdminPage)
		return
	}
	stats, err := storage.GetPlatformStats(c.Request.Context(), h.db)
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

func adminPagination(raw string) (int, int, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return 0, 0, storage.ErrInvalidAdminPage
	}
	limit, offset := 20, 0
	for key, entries := range values {
		if (key != "limit" && key != "offset") || len(entries) != 1 || entries[0] == "" {
			return 0, 0, storage.ErrInvalidAdminPage
		}
		for _, digit := range entries[0] {
			if digit < '0' || digit > '9' {
				return 0, 0, storage.ErrInvalidAdminPage
			}
		}
		value, parseErr := strconv.Atoi(entries[0])
		if parseErr != nil {
			return 0, 0, storage.ErrInvalidAdminPage
		}
		if key == "limit" {
			limit = value
		} else {
			offset = value
		}
	}
	return limit, offset, nil
}

func adminFailure(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "admin_unavailable", "Administrator data unavailable"
	switch {
	case errors.Is(err, storage.ErrInvalidAdminPage):
		status, code, message = http.StatusBadRequest, "invalid_admin_query", "Use only limit (1-100) and offset (0-1000000) on the users list"
	case errors.Is(err, storage.ErrUserNotFound):
		status, code, message = http.StatusNotFound, "user_not_found", "User not found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusRequestTimeout, "request_timeout", "Request cancelled or timed out"
	default:
		customLog.Warnf("Platform admin read failed: %v", err)
	}
	c.AbortWithStatusJSON(status, gin.H{"error": message, "code": code})
}
