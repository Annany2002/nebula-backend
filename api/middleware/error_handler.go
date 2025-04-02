// api/middleware/error_handler.go
package middleware

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10" // Import validator for binding errors

	"github.com/Annany2002/nebula-backend/internal/auth"    // Import internal auth errors
	"github.com/Annany2002/nebula-backend/internal/storage" // Import internal storage errors
)

// ErrorHandler creates a Gin middleware for centralized error handling.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request using subsequent handlers
		c.Next()

		// Check if any errors were attached during handler execution
		if len(c.Errors) == 0 {
			return // No errors, nothing to do
		}

		// We only handle the last error for the response.
		// Gin stores errors in c.Errors []*gin.Error.
		err := c.Errors.Last().Err

		// Log the error internally for debugging purposes
		// Consider using a more structured logger here in a real app
		log.Printf("[ErrorHandler] Detected error: %v | Type: %T", err, err)

		// --- Map error to HTTP status code and user message ---
		var statusCode int
		var userMessage string

		// Check for specific error types we defined
		if errors.Is(err, storage.ErrUserNotFound) ||
			errors.Is(err, storage.ErrDatabaseNotFound) ||
			errors.Is(err, storage.ErrRecordNotFound) ||
			errors.Is(err, storage.ErrTableNotFound) {
			statusCode = http.StatusNotFound
			userMessage = err.Error() // Use the error message directly for "Not Found" types
		} else if errors.Is(err, storage.ErrEmailExists) ||
			errors.Is(err, storage.ErrDatabaseExists) ||
			errors.Is(err, storage.ErrConstraintViolation) {
			statusCode = http.StatusConflict
			userMessage = err.Error() // Use the error message directly for conflicts
		} else if errors.Is(err, auth.ErrTokenMalformed) ||
			errors.Is(err, auth.ErrTokenInvalid) ||
			errors.Is(err, auth.ErrTokenClaimsInvalid) ||
			errors.Is(err, auth.ErrUnexpectedSigningMethod) {
			statusCode = http.StatusUnauthorized
			userMessage = "Invalid or malformed authentication token." // Generic message
		} else if errors.Is(err, auth.ErrTokenExpired) {
			statusCode = http.StatusUnauthorized
			userMessage = "Authentication token has expired."
		} else if validationErrs, ok := err.(validator.ValidationErrors); ok {
			// Handle validation errors from c.ShouldBindJSON
			statusCode = http.StatusBadRequest
			// Create a user-friendly message (can be more detailed)
			userMessage = "Validation failed. Please check your input."
			// Optional: Log details of validation errors
			for _, fe := range validationErrs {
				log.Printf("Validation Error: Field %s failed on %s", fe.Field(), fe.Tag())
			}
		} else if errors.Is(err, storage.ErrColumnNotFound) ||
			errors.Is(err, storage.ErrTypeMismatch) {
			// Treat schema mismatches or bad input types as Bad Request
			statusCode = http.StatusBadRequest
			userMessage = err.Error()
		} else {
			// --- Default/Fallback for unhandled errors ---
			// Check for other common error indicators if needed, e.g., context deadline exceeded

			// Assume internal server error for unexpected types
			statusCode = http.StatusInternalServerError
			userMessage = "An unexpected internal server error occurred."
			// Log the original error for investigation
			log.Printf("Unhandled error type: %T, Error: %v", err, err)
		}

		// Abort execution and send JSON response
		// Ensure response hasn't already been sent (Gin usually handles this with Abort)
		if !c.Writer.Written() {
			c.AbortWithStatusJSON(statusCode, gin.H{"error": userMessage})
		} else {
			log.Printf("[ErrorHandler] Warning: Response already written before handling error.")
		}
	}
}
