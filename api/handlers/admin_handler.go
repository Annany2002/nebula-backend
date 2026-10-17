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

// AdminHandler exposes account management and bounded schema statistics behind the admin gate.
type AdminHandler struct {
	db          *sql.DB
	directory   string
	statsPermit chan struct{}
}

// NewAdminHandler shares the existing metadata connection pool.
func NewAdminHandler(db *sql.DB, directory string) *AdminHandler {
	return &AdminHandler{db: db, directory: directory, statsPermit: make(chan struct{}, 1)}
}

// ListUsers returns a bounded page with no credential-bearing account fields.
func (h *AdminHandler) ListUsers(c *gin.Context) {
	values, limit, offset, err := adminListQuery(c.Request.URL.RawQuery, "q", "role", "status")
	if err != nil {
		adminFailure(c, err)
		return
	}
	filter := storage.AdminUserFilter{Query: values.Get("q"), Role: values.Get("role"), Status: values.Get("status")}
	users, total, err := storage.ListAdminUsers(c.Request.Context(), h.db, limit, offset, filter)
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, models.AdminUserListResponse{Users: users, Pagination: models.AdminPagination{Total: total, Limit: limit, Offset: offset}})
}

// GetUser returns an account's safe metadata, without access to its tenant data.
func (h *AdminHandler) GetUser(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		adminFailure(c, storage.ErrInvalidAdminQuery)
		return
	}
	user, err := storage.GetAdminUser(c.Request.Context(), h.db, c.Param("user_id"))
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// Stats serializes bounded schema inspections so admin traffic cannot exhaust file handles.
func (h *AdminHandler) Stats(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		adminFailure(c, storage.ErrInvalidAdminQuery)
		return
	}
	select {
	case h.statsPermit <- struct{}{}:
		defer func() { <-h.statsPermit }()
	default:
		c.Header("Retry-After", "1")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Platform statistics are busy", "code": "admin_stats_busy"})
		return
	}
	stats, err := storage.GetPlatformStats(c.Request.Context(), h.db, h.directory)
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// AuditEvents exposes existing suspension/reactivation history to administrators only.
func (h *AdminHandler) AuditEvents(c *gin.Context) {
	values, limit, offset, err := adminListQuery(c.Request.URL.RawQuery, "actor_user_id", "target_user_id", "action")
	if err != nil {
		adminFailure(c, err)
		return
	}
	filter := storage.AdminAuditFilter{ActorUserID: values.Get("actor_user_id"), TargetUserID: values.Get("target_user_id"), Action: values.Get("action")}
	events, total, err := storage.ListAdminAuditEvents(c.Request.Context(), h.db, limit, offset, filter)
	if err != nil {
		adminFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, models.AdminAuditListResponse{Events: events, Pagination: models.AdminPagination{Total: total, Limit: limit, Offset: offset}})
}

func adminListQuery(raw string, filters ...string) (url.Values, int, int, error) {
	if len(raw) > 2048 {
		return nil, 0, 0, storage.ErrInvalidAdminQuery
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, 0, 0, storage.ErrInvalidAdminQuery
	}
	allowed := map[string]bool{"limit": true, "offset": true}
	for _, key := range filters {
		allowed[key] = true
	}
	limit, offset := 20, 0
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 || entries[0] == "" {
			return nil, 0, 0, storage.ErrInvalidAdminQuery
		}
		if key != "limit" && key != "offset" {
			continue
		}
		for _, digit := range entries[0] {
			if digit < '0' || digit > '9' {
				return nil, 0, 0, storage.ErrInvalidAdminQuery
			}
		}
		value, parseErr := strconv.Atoi(entries[0])
		if parseErr != nil {
			return nil, 0, 0, storage.ErrInvalidAdminQuery
		}
		if key == "limit" {
			limit = value
		} else {
			offset = value
		}
	}
	return values, limit, offset, nil
}

func adminFailure(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "admin_unavailable", "Administrator data unavailable"
	switch {
	case errors.Is(err, storage.ErrAdminRequired):
		status, code, message = http.StatusForbidden, "admin_required", "Active platform administrator access required"
	case errors.Is(err, storage.ErrAdminProtected):
		status, code, message = http.StatusConflict, "admin_account_protected", "Revoke administrator membership before suspending this account"
	case errors.Is(err, storage.ErrAdminSessionRevoked):
		status, code, message = http.StatusUnauthorized, "session_revoked", "Sign in again to continue"
	case errors.Is(err, storage.ErrInvalidAdminQuery):
		status, code, message = http.StatusBadRequest, "invalid_admin_query", "Invalid administrator query parameters"
	case errors.Is(err, storage.ErrUserNotFound):
		status, code, message = http.StatusNotFound, "user_not_found", "User not found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusRequestTimeout, "request_timeout", "Request cancelled or timed out"
	default:
		customLog.Warnf("Platform admin read failed: %v", err)
	}
	c.AbortWithStatusJSON(status, gin.H{"error": message, "code": code})
}
