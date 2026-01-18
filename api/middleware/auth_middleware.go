// api/middleware/auth_middleware.go
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth" // Import internal auth logic and errors
)

// AuthMiddleware creates a gin middleware for checking JWT authentication.
// It depends on the application configuration for the JWT secret.
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			err := errors.New("authorization header required")
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			err := errors.New("authorization header format must be Bearer {token}")
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		tokenString := parts[1]

		// Validate JWT using the internal auth function
		userId, err := auth.ValidateJWT(tokenString, cfg.JWTSecret)

		if err != nil {
			customLog.Printf("AuthMiddleware: Token validation failed: %v", err)
			statusCode := http.StatusUnauthorized
			errMsg := "Invalid token"
			switch {
			case errors.Is(err, auth.ErrTokenMalformed):
				errMsg = err.Error()
			case errors.Is(err, auth.ErrTokenExpired):
				errMsg = err.Error()
			}

			_ = c.Error(err)
			c.AbortWithStatusJSON(statusCode, gin.H{"error": errMsg})
			return
		}

		// Token is valid! Set the userID in the context
		customLog.Printf("AuthMiddleware: Token validated successfully for UserID: %s", userId)
		c.Set("userId", userId) // Use consistent key

		c.Next() // Continue to the next handler
	}
}
