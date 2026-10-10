package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// BackupHandler translates HTTP contracts; the store owns snapshot lifecycle rules.
type BackupHandler struct{ store *storage.BackupStore }

// NewBackupHandler creates one store shared by this router's backup endpoints.
func NewBackupHandler(meta *sql.DB, directory string) *BackupHandler {
	return &BackupHandler{store: storage.NewBackupStore(meta, directory)}
}

func backupOwner(c *gin.Context) (string, bool) {
	if key, _ := c.Get("isApiKey"); key == true {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Backup management requires an owner JWT.", "code": "owner_jwt_required"})
		return "", false
	}
	return c.MustGet("userId").(string), true
}

func backupContext(c *gin.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	deadline, _ := ctx.Deadline()
	controller := http.NewResponseController(c.Writer)
	// A slow request body or download must not outlive the operation's budget.
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
	return ctx, func() {
		cancel()
		_ = controller.SetReadDeadline(time.Time{})
		_ = controller.SetWriteDeadline(time.Time{})
	}
}

func decodeBackupRequest(c *gin.Context, target any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		c.Header("Connection", "close")
		_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now())
		c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "Use application/json.", "code": "unsupported_media_type"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(target)
	if err == nil {
		var extra any
		if tailErr := decoder.Decode(&extra); !errors.Is(tailErr, io.EOF) {
			if tailErr == nil {
				tailErr = storage.ErrInvalidBackupRequest
			}
			err = tailErr
		}
	}
	if err == nil {
		return true
	}
	status, code := http.StatusBadRequest, "invalid_backup_request"
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, code = http.StatusRequestEntityTooLarge, "request_too_large"
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		status, code = http.StatusRequestTimeout, "request_timeout"
	}
	// Retire rejected unread bodies without poisoning a reusable connection's context.
	c.Header("Connection", "close")
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now())
	c.AbortWithStatusJSON(status, gin.H{"error": "Invalid JSON backup request.", "code": code})
	return false
}

// Create saves a snapshot or returns the original completed creation for its UUID.
func (h *BackupHandler) Create(c *gin.Context) {
	owner, ok := backupOwner(c)
	if !ok {
		return
	}
	ctx, finish := backupContext(c)
	defer finish()
	var request models.CreateBackupRequest
	if !decodeBackupRequest(c, &request) {
		return
	}
	backup, created, err := h.store.Create(ctx, owner, c.Param("db_name"), request.BackupID)
	if err != nil {
		h.fail(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Header("Location", "/api/v1/backups/"+backup.BackupID)
	c.JSON(status, models.BackupResponse{Backup: backup})
}

// List is account-scoped so history stays accessible after deleting a source database.
func (h *BackupHandler) List(c *gin.Context) {
	owner, ok := backupOwner(c)
	if !ok {
		return
	}
	ctx, finish := backupContext(c)
	defer finish()
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		h.fail(c, storage.ErrInvalidBackupRequest)
		return
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "db_name" && key != "limit" && key != "offset") {
			h.fail(c, storage.ErrInvalidBackupRequest)
			return
		}
	}
	limit, err := backupListNumber(values, "limit", 20)
	if err != nil {
		h.fail(c, err)
		return
	}
	offset, err := backupListNumber(values, "offset", 0)
	if err != nil {
		h.fail(c, err)
		return
	}
	backups, total, err := h.store.List(ctx, owner, values.Get("db_name"), limit, offset)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups, "pagination": storage.PaginationMeta{Total: total, Limit: limit, Offset: offset}})
}

func backupListNumber(values url.Values, key string, fallback int) (int, error) {
	if !values.Has(key) {
		return fallback, nil
	}
	value := values.Get(key)
	if value == "" {
		return 0, storage.ErrInvalidBackupRequest
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, storage.ErrInvalidBackupRequest
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, storage.ErrInvalidBackupRequest
	}
	return number, nil
}

// Get returns owner-visible metadata for one backup.
func (h *BackupHandler) Get(c *gin.Context) {
	owner, ok := backupOwner(c)
	if !ok {
		return
	}
	ctx, finish := backupContext(c)
	defer finish()
	backup, err := h.store.Get(ctx, owner, c.Param("backup_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, models.BackupResponse{Backup: backup})
}

// Download streams verified bytes from the opened handle rather than reopening a path.
func (h *BackupHandler) Download(c *gin.Context) {
	owner, ok := backupOwner(c)
	if !ok {
		return
	}
	ctx, finish := backupContext(c)
	defer finish()
	backup, file, err := h.store.Open(ctx, owner, c.Param("backup_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	defer file.Close() //nolint:errcheck // Read-only snapshot.
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+backup.BackupID+`.db"`)
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, backup.SizeBytes, "application/octet-stream", io.NewSectionReader(file, 0, backup.SizeBytes), nil)
}

// Restore publishes a new database, leaving both the source and saved backup intact.
func (h *BackupHandler) Restore(c *gin.Context) {
	owner, ok := backupOwner(c)
	if !ok {
		return
	}
	ctx, finish := backupContext(c)
	defer finish()
	var request models.RestoreBackupRequest
	if !decodeBackupRequest(c, &request) {
		return
	}
	id := c.Param("backup_id")
	size, err := h.store.Restore(ctx, owner, id, request.DBName)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Location", "/api/v1/databases/"+request.DBName)
	c.JSON(http.StatusCreated, models.RestoreBackupResponse{Message: "Backup restored successfully", BackupID: id, DBName: request.DBName, SizeBytes: size})
}

// Delete is idempotent for owner-visible missing IDs.
func (h *BackupHandler) Delete(c *gin.Context) {
	owner, ok := backupOwner(c)
	if !ok {
		return
	}
	ctx, finish := backupContext(c)
	defer finish()
	if err := h.store.Delete(ctx, owner, c.Param("backup_id")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *BackupHandler) fail(c *gin.Context, err error) {
	status, message, code := http.StatusInternalServerError, "Backup operation failed.", "backup_storage_error"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message, code = http.StatusRequestTimeout, "Backup operation timed out or was cancelled.", "request_timeout"
	case errors.Is(err, storage.ErrInvalidBackupRequest), errors.Is(err, storage.ErrInvalidSnapshot):
		status, message, code = http.StatusBadRequest, "Invalid backup request or unsupported database snapshot.", "invalid_backup_request"
	case errors.Is(err, storage.ErrBackupNotFound), errors.Is(err, storage.ErrDatabaseNotFound):
		status, message, code = http.StatusNotFound, "Backup or source database not found.", "backup_not_found"
	case errors.Is(err, storage.ErrDatabaseExists):
		status, message, code = http.StatusConflict, "Destination database already exists.", "database_exists"
	case errors.Is(err, storage.ErrBackupConflict):
		status, message, code = http.StatusConflict, "Backup identifier is already in use. Use a new UUID for a new backup.", "backup_id_conflict"
	case errors.Is(err, storage.ErrBackupQuota):
		status, message, code = http.StatusConflict, "Backup count or storage limit reached. Delete an old backup first.", "backup_quota_exceeded"
	case errors.Is(err, storage.ErrBackupTooLarge):
		status, message, code = http.StatusRequestEntityTooLarge, "Snapshots must not exceed 64 MiB.", "snapshot_too_large"
	case errors.Is(err, storage.ErrBackupUnavailable):
		status, message, code = http.StatusConflict, "Snapshot file is unavailable or failed integrity verification.", "backup_unavailable"
	case errors.Is(err, storage.ErrBackupBusy):
		status, message, code = http.StatusServiceUnavailable, "Backup operation is busy. Check its status before retrying.", "backup_busy"
		c.Header("Retry-After", "5")
	}
	if status == http.StatusInternalServerError {
		customLog.Warnf("Backup operation failed: %v", err)
	}
	c.AbortWithStatusJSON(status, gin.H{"error": message, "code": code})
}
