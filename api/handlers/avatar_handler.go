package handlers

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/avatar"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// Bound concurrent image decoding; upload body sizes are bounded independently.
var avatarSlots = make(chan struct{}, 2)

func (h *AuthHandler) UploadAvatar(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	media, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "multipart/form-data" || params["boundary"] == "" {
		c.JSON(415, gin.H{"error": "Use multipart/form-data with one avatar file", "code": "invalid_avatar"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, avatar.MaxUploadBytes+64*1024))
	if err != nil {
		c.JSON(413, gin.H{"error": "Avatar upload is too large", "code": "avatar_too_large"})
		return
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "avatar" || part.FileName() == "" {
		c.JSON(400, gin.H{"error": "Provide one avatar file", "code": "invalid_avatar"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(part, avatar.MaxUploadBytes+1))
	if err != nil {
		c.JSON(400, gin.H{"error": "Image could not be read", "code": "invalid_avatar"})
		return
	}
	if len(data) > avatar.MaxUploadBytes {
		c.JSON(413, gin.H{"error": "Avatar upload is too large", "code": "avatar_too_large"})
		return
	}
	if _, err := reader.NextPart(); !errors.Is(err, io.EOF) {
		c.JSON(400, gin.H{"error": "Provide only one avatar file", "code": "invalid_avatar"})
		return
	}
	select {
	case avatarSlots <- struct{}{}:
	default:
		c.JSON(503, gin.H{"error": "Image processing is busy; try again", "code": "avatar_busy"})
		return
	}
	defer func() { <-avatarSlots }()

	normalized, err := avatar.Normalize(data)
	if err != nil {
		c.JSON(400, gin.H{"error": avatar.ErrImage.Error(), "code": "invalid_avatar"})
		return
	}
	if err := storage.SaveAvatar(c.Request.Context(), h.DB, c.MustGet("userId").(string), normalized); err != nil {
		_ = c.Error(err)
		return
	}
	h.GetCurrentUser(c)
}

func (h *AuthHandler) DeleteAvatar(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := storage.RemoveAvatar(c.Request.Context(), h.DB, c.MustGet("userId").(string)); err != nil {
		_ = c.Error(err)
		return
	}
	h.GetCurrentUser(c)
}

func (h *AuthHandler) CompleteOnboarding(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := storage.CompleteOnboarding(c.Request.Context(), h.DB, c.MustGet("userId").(string)); err != nil {
		_ = c.Error(err)
		return
	}
	h.GetCurrentUser(c)
}

// PublicAvatar serves only normalized JPEG bytes at a random asset ID, never filesystem paths.
// Profile images are public. Replacement, removal or account deletion revokes the old asset ID.
func (h *AuthHandler) PublicAvatar(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	content, err := storage.ReadAvatar(c.Request.Context(), h.DB, c.Param("avatar_id"))
	if errors.Is(err, storage.ErrUserNotFound) {
		c.Status(404)
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, "image/jpeg", content)
}
