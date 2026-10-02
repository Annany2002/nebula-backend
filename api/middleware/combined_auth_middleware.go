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

			owner, id, err := storage.AuthenticateAPIKey(c.Request.Context(), db, credentials)
			if err != nil {
				if !errors.Is(err, storage.ErrAPIKeyNotFound) {
					customLog.Warnf("API key lookup failed: %v", err)
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Authentication is unavailable"})
					return
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
				return
			}
			userId, databaseId = owner, id

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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unsupported authentication scheme"})
			return
		}

		if isApiKeyAuth && c.FullPath() != "/api/v1/health" {
			dbName := c.Param("db_name")
			if dbName == "" || (c.Request.Method == http.MethodDelete && c.FullPath() == "/api/v1/databases/:db_name") {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "API keys only allow scoped database data operations"})
				return
			}
			targetID, err := storage.FindDatabaseIDByNameAndUser(c.Request.Context(), db, userId, dbName)
			if err != nil || targetID != databaseId.(int64) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "API key is not valid for this database"})
				return
			}
		}

		// --- Authentication Success ---
		customLog.Printf("CombinedAuthMiddleware: Auth success. UserID: %s, DatabaseID: %v (Scheme: %s)\n", userId, databaseId, scheme)
		c.Set("userId", userId)
		c.Set("databaseId", databaseId) // Will be int64 for DB-scoped ApiKey, nil for JWT

		c.Next() // Proceed to the next handler

	}
}
