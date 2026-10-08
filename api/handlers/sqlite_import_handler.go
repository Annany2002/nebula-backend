package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/core"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

const maxImportRequestBytes = storage.MaxSQLiteImportBytes + (64 << 10)

// Limit simultaneous uploads/validation across handlers in this server process.
var sqliteImportSlots = make(chan struct{}, 2)

// ImportSQLite handles one standalone SQLite snapshot and one destination name.
func (h *DatabaseHandler) ImportSQLite(c *gin.Context) {
	if isAPIKey, _ := c.Get("isApiKey"); isAPIKey == true {
		rejectSQLiteImport(c, http.StatusForbidden, "Importing a new database requires an owner JWT.")
		return
	}
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		rejectSQLiteImport(c, http.StatusUnsupportedMediaType, "Use multipart/form-data with db_name and file fields.")
		return
	}
	if c.Request.ContentLength > maxImportRequestBytes {
		rejectSQLiteImport(c, http.StatusRequestEntityTooLarge, "SQLite snapshots must not exceed 64 MiB.")
		return
	}
	select {
	case sqliteImportSlots <- struct{}{}:
		defer func() { <-sqliteImportSlots }()
	default:
		c.Header("Retry-After", "5")
		rejectSQLiteImport(c, http.StatusServiceUnavailable, "Database import is busy. Try again shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	controller := http.NewResponseController(c.Writer)
	deadline, _ := ctx.Deadline()
	// Gin exposes Unwrap; real HTTP writers support read deadlines. Test writers
	// may not, so cancellation also closes the request body below.
	if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.importError(c, err)
		return
	}
	done := make(chan struct{})
	watcherFinished := make(chan struct{})
	defer func() {
		close(done)
		<-watcherFinished
		_ = controller.SetReadDeadline(time.Time{})
	}()
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxImportRequestBytes)
	// Successful uploads are fully consumed below. Rejections expire the read
	// deadline in rejectSQLiteImport and cannot wait on an unfinished body.
	defer func() { _ = body.Close() }()
	go func() {
		defer close(watcherFinished)
		select {
		case <-ctx.Done():
			_ = controller.SetReadDeadline(time.Now())
			_ = body.Close()
		case <-done:
		}
	}()
	stage, err := os.CreateTemp(h.Cfg.MetadataDbDir, ".nebula-import-*.db")
	if err != nil {
		h.importError(c, err)
		return
	}
	defer func() {
		_ = stage.Close()
		if err := os.Remove(stage.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			customLog.Warnf("Import staging cleanup failed: %v", err)
		}
	}()
	name, err := readSQLiteUpload(ctx, multipart.NewReader(body, params["boundary"]), stage)
	if err == nil {
		_, err = io.Copy(io.Discard, body) // Enforce the total body limit, including multipart epilogue.
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		h.importError(c, err)
		return
	}
	if err := stage.Sync(); err != nil {
		h.importError(c, err)
		return
	}
	if err := stage.Close(); err != nil {
		h.importError(c, err)
		return
	}
	validationCtx, stopValidation := context.WithTimeout(ctx, 30*time.Second)
	defer stopValidation()
	size, err := storage.ImportSQLiteDatabase(validationCtx, h.MetaDB, h.Cfg.MetadataDbDir, c.MustGet("userId").(string), name, stage.Name())
	if err != nil {
		h.importError(c, err)
		return
	}
	c.JSON(http.StatusCreated, models.ImportSQLiteResponse{Message: "Database imported successfully", DBName: name, SizeBytes: size})
}

func readSQLiteUpload(ctx context.Context, reader *multipart.Reader, destination io.Writer) (string, error) {
	var name string
	seen := make(map[string]bool, 2)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", importUploadError(err)
		}
		field := part.FormName()
		if seen[field] || (field != "db_name" && field != "file") {
			return "", fmt.Errorf("%w: exactly one db_name and one file are required", storage.ErrInvalidSnapshot)
		}
		seen[field] = true
		switch field {
		case "db_name":
			data, err := io.ReadAll(io.LimitReader(part, 65))
			if err != nil {
				return "", importUploadError(err)
			}
			name = string(data)
			if part.FileName() != "" || !core.IsValidIdentifier(name) {
				return "", fmt.Errorf("%w: db_name must contain 1–64 letters, digits or underscores", storage.ErrInvalidSnapshot)
			}
		case "file":
			if part.FileName() == "" {
				return "", fmt.Errorf("%w: file must be a file upload", storage.ErrInvalidSnapshot)
			}
			// Never use the supplied filename as a filesystem path, or buffer the file in memory.
			size, err := io.Copy(destination, io.LimitReader(part, storage.MaxSQLiteImportBytes+1))
			if size > storage.MaxSQLiteImportBytes {
				return "", &http.MaxBytesError{Limit: storage.MaxSQLiteImportBytes}
			}
			if err != nil {
				return "", importUploadError(err)
			}
		}
		if err := part.Close(); err != nil {
			return "", importUploadError(err)
		}
	}
	if !seen["db_name"] || !seen["file"] {
		return "", fmt.Errorf("%w: db_name and file are required", storage.ErrInvalidSnapshot)
	}
	return name, nil
}

func importUploadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return err
	}
	return fmt.Errorf("%w: malformed or incomplete multipart upload", storage.ErrInvalidSnapshot)
}

func (h *DatabaseHandler) importError(c *gin.Context, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		rejectSQLiteImport(c, http.StatusRequestEntityTooLarge, "SQLite snapshots must not exceed 64 MiB.")
	case errors.Is(err, storage.ErrInvalidSnapshot):
		rejectSQLiteImport(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, storage.ErrDatabaseExists):
		rejectSQLiteImport(c, http.StatusConflict, "A database registration or file with this name already exists. Choose a new name.")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		rejectSQLiteImport(c, http.StatusRequestTimeout, "Import interrupted or timed out. Refresh your database list before trying again.")
	default:
		customLog.Warnf("SQLite import failed: %v", err)
		rejectSQLiteImport(c, http.StatusInternalServerError, "Failed to import database.")
	}
}

func rejectSQLiteImport(c *gin.Context, status int, message string) {
	// net/http can drain unread request bytes when starting a response. Expire the
	// read deadline first so a rejected, unfinished upload cannot delay its response.
	// A failed background read can cancel the HTTP/1 connection context permanently;
	// retire that connection instead of letting later requests reuse a cancelled context.
	c.Header("Connection", "close")
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now())
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}
