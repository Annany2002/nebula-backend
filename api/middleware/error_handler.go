// api/middleware/error_handler.go
package middleware

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// ErrorHandler creates a Gin middleware for centralized error handling.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next() // Process request

		if len(c.Errors) == 0 {
			return
		} // No errors

		err := c.Errors.Last().Err
		customLog.Warnf("[ErrorHandler] Detected error: %v | Type: %T", err, err)

		var statusCode int
		var userMessage string

		// --- Map error to HTTP status code and user message ---
		if errors.Is(err, storage.ErrUserNotFound) ||
			errors.Is(err, storage.ErrDatabaseNotFound) ||
			errors.Is(err, storage.ErrRecordNotFound) ||
			errors.Is(err, storage.ErrTableNotFound) {
			statusCode = http.StatusNotFound
			userMessage = err.Error()
			// *** NEW: Check for Invalid Credentials ***
		} else if errors.Is(err, storage.ErrInvalidCredentials) {
			statusCode = http.StatusUnauthorized       // Map to 401
			userMessage = "Invalid email or password." // Generic message
			// *** END NEW ***
		} else if errors.Is(err, storage.ErrEmailExists) ||
			errors.Is(err, storage.ErrDatabaseExists) ||
			errors.Is(err, storage.ErrConstraintViolation) {
			statusCode = http.StatusConflict
			userMessage = err.Error()
		} else if errors.Is(err, auth.ErrTokenMalformed) ||
			errors.Is(err, auth.ErrTokenInvalid) ||
			errors.Is(err, auth.ErrTokenClaimsInvalid) ||
			errors.Is(err, auth.ErrUnexpectedSigningMethod) {
			statusCode = http.StatusUnauthorized // Keep as 401
			userMessage = "Invalid or malformed authentication token."
		} else if errors.Is(err, auth.ErrTokenExpired) {
			statusCode = http.StatusUnauthorized // Keep as 401
			userMessage = "Authentication token has expired."
		} else if validationErrs, ok := err.(validator.ValidationErrors); ok {
			statusCode = http.StatusBadRequest
			userMessage = "Validation failed. Please check your input."
			// Log details optional
			for _, fe := range validationErrs {
				customLog.Warnf("Validation Error: Field %s failed on %s", fe.Field(), fe.Tag())
			}
		} else if errors.Is(err, storage.ErrColumnNotFound) ||
			errors.Is(err, storage.ErrTypeMismatch) ||
			errors.Is(err, storage.ErrInvalidFilterValue) { // Include filter value error
			statusCode = http.StatusBadRequest
			userMessage = err.Error()
		} else {
			// --- Default/Fallback ---
			statusCode = http.StatusInternalServerError
			userMessage = "An unexpected internal server error occurred."
			customLog.Warnf("Unhandled error type: %T, Error: %v", err, err)
		}

		// Abort and send JSON response if not already sent
		if !c.Writer.Written() {
			c.AbortWithStatusJSON(statusCode, gin.H{"error": userMessage})
		} else {
			log.Printf("[ErrorHandler] Warning: Response already written before handling error.")
		}
	}
}
