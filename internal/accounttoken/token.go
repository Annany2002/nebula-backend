// Package accounttoken generates opaque account-action tokens and stores only their digest.
package accounttoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// New returns a cryptographically random 256-bit URL-safe token.
func New() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Valid rejects malformed and non-canonical encodings before storage access.
func Valid(token string) bool {
	value, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(value) == 32 && base64.RawURLEncoding.EncodeToString(value) == token
}

// Digest is the only representation retained in metadata storage.
func Digest(token string) string {
	value := sha256.Sum256([]byte(token))
	return hex.EncodeToString(value[:])
}
