package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

func decodeAccountJSONBody(c *gin.Context, target any) bool {
	c.Header("Cache-Control", "no-store")
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		c.JSON(415, gin.H{"error": "Use application/json", "code": "unsupported_media_type"})
		return false
	}
	return decodeAccountBody(c, target, true)
}

// decodeAccountBody bounds the complete payload, including content after the first object.
func decodeAccountBody(c *gin.Context, target any, strict bool) bool {
	c.Header("Cache-Control", "no-store")
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		writeAuthBindingError(c, err)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if strict {
		decoder.DisallowUnknownFields()
	}
	err = decoder.Decode(target)
	if err == nil {
		var extra any
		if tail := decoder.Decode(&extra); tail != io.EOF {
			err = errors.New("unexpected JSON content")
		}
	}
	if err != nil {
		writeAuthBindingError(c, err)
		return false
	}
	if err := binding.Validator.ValidateStruct(target); err != nil {
		writeAuthBindingError(c, err)
		return false
	}
	return true
}

func writeAuthBindingError(c *gin.Context, err error) {
	status, code := 400, "invalid_auth_request"
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, code = 413, "request_too_large"
	}
	c.JSON(status, gin.H{"error": "Invalid account request", "code": code})
}
