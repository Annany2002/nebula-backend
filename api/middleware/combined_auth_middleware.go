package middleware

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

var (
	customLog     = logger.NewLogger()
	authKeyPrefix = "neb_" // nolint:gosec // Not a credential, just a prefix
)

// This middleware checks requests coming using either from the bearer or the api key token
// within the Authorization Header
func CombinedAuthMiddleware(db *sql.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No Authorization header provided at all
			err := auth.ErrUnauthorized
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			// Invalid header format (not "Scheme Credentials")
			err := fmt.Errorf("%w: invalid header format", auth.ErrTokenMalformed)
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header format must be 'Bearer {token}' or 'ApiKey {key}'"})
			return
		}

		scheme := strings.ToLower(parts[0])
		credentials := parts[1]

		var userId string
		var databaseId any
		var isApiKeyAuth bool

		// --- Try Different Authentication Schemes ---
		switch scheme {
		case "apikey":
			customLog.Println("CombinedAuthMiddleware: Attempting ApiKey authentication...")
			if !strings.HasPrefix(credentials, authKeyPrefix) {
				_ = c.Error(fmt.Errorf("%w: invalid key prefix", auth.ErrTokenMalformed))
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
				return
			}

			// Find database ID from the API key
			apiKeyQuery := `SELECT api_database_id, api_owner_id FROM api_keys WHERE key = ?`
			row := db.QueryRow(apiKeyQuery, credentials)

			err := row.Scan(&databaseId, &userId)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					_ = c.Error(fmt.Errorf("%w: invalid API key", auth.ErrTokenMalformed))
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
					return
				}
				customLog.Warnf("error scanning databaseId: %v", err)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key format"})
				return
			}

			apiKey, err := storage.FindAPIKeyByDatabaseId(c.Request.Context(), db, databaseId.(int64))
			if err != nil {
				customLog.Warnf("CombinedAuthMiddleware: DB error looking up ApiKey for database ID '%d': %v", databaseId.(int64), err)
				_ = c.Error(fmt.Errorf("internal error during auth: %w", err))
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key format"})
				return
			}
			if apiKey == "" {
				_ = c.Error(auth.ErrUnauthorized)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
				return
			}

			isApiKeyAuth = true
			c.Set("isApiKey", isApiKeyAuth)

		case "bearer":
			customLog.Println("CombinedAuthMiddleware: Attempting Bearer token authentication...")
			jwtUserID, jwtErr := auth.ValidateJWT(credentials, cfg.JWTSecret)
			if jwtErr != nil {
				customLog.Printf("AuthMiddleware: Token validation failed: %v", jwtErr)
				statusCode := http.StatusUnauthorized
				errMsg := "Invalid token"
				switch {
				case errors.Is(jwtErr, auth.ErrTokenMalformed):
					errMsg = jwtErr.Error()
				case errors.Is(jwtErr, auth.ErrTokenExpired):
					errMsg = jwtErr.Error()
				}

				_ = c.Error(jwtErr)
				c.AbortWithStatusJSON(statusCode, gin.H{"error": errMsg})
				return
			}

			userId = jwtUserID
			databaseId = nil // Explicitly set databaseID to nil for JWT/user scope

		default:
			// Unsupported authentication scheme
			defaultErr := fmt.Errorf("%w: unsupported scheme '%s'", auth.ErrTokenMalformed, parts[0])
			customLog.Warnf("CombinedAuthMiddleware: Authentication failed (Scheme: %s): %v", scheme, defaultErr)
			_ = c.Error(defaultErr)
			c.Abort()
			return
		}

		// --- Authentication Success ---
		customLog.Printf("CombinedAuthMiddleware: Auth success. UserID: %s, DatabaseID: %v (Scheme: %s)\n", userId, databaseId, scheme)
		c.Set("userId", userId)
		c.Set("databaseId", databaseId) // Will be int64 for DB-scoped ApiKey, nil for JWT

		c.Next() // Proceed to the next handler

	}
}
