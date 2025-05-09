package middleware

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth"
	nebulaErrors "github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage"
	"github.com/gin-gonic/gin"
)

var (
	customLog    = logger.NewLogger()
	apiKeyPrefix = "neb_"
)

// This middleware checks requests coming using either from the bearer or the api key token
// within the Authorization Header
func CombinedAuthMiddleware(db *sql.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No Authorization header provided at all
			err := nebulaErrors.ErrUnauthorized
			c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			// Invalid header format (not "Scheme Credentials")
			err := fmt.Errorf("%w: invalid header format", nebulaErrors.ErrTokenMalformed)
			c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header format must be 'Bearer {token}' or 'ApiKey {key}'"})
			return
		}

		scheme := strings.ToLower(parts[0])
		credentials := parts[1]

		var userId string
		var databaseId any
		var authErr error
		var isApiKeyAuth bool = false

		// --- Try Different Authentication Schemes ---
		switch scheme {
		case "apikey":
			customLog.Println("CombinedAuthMiddleware: Attempting ApiKey authentication...")
			if !strings.HasPrefix(credentials, apiKeyPrefix) {
				authErr = fmt.Errorf("%w: invalid key prefix", nebulaErrors.ErrTokenMalformed)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
				return
			}

			// Find database ID from the API key
			apiKeyQuery := `SELECT api_database_id, api_owner_id FROM api_keys WHERE key = ?`
			row := db.QueryRow(apiKeyQuery, credentials)

			err := row.Scan(&databaseId, &userId)
			if err != nil {
				if err == sql.ErrNoRows {
					authErr = fmt.Errorf("%w: invalid API key", nebulaErrors.ErrTokenMalformed)
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
					return
				}
				customLog.Fatalf("error scanning databaseId: %v", err)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key format"})
				return
			}

			// keyParts := strings.SplitN(credentials, "_", 2)
			// customLog.Printf("keyParts is %v", keyParts)
			// if len(keyParts) != 2 {
			// 	authErr = fmt.Errorf("%w: inval	id key format", nebulaErrors.ErrTokenMalformed)
			// 	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key format"})
			// 	return
			// }

			// secretPart := keyParts[1]
			api_key, err := storage.FindAPIKeyByDatabaseId(c.Request.Context(), db, databaseId.(int64))
			if err != nil {
				customLog.Warnf("CombinedAuthMiddleware: DB error looking up ApiKey for database ID '%d': %v", databaseId.(int64), err)
				authErr = fmt.Errorf("internal error during auth: %w", err)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key format"})
				return
			}
			if len(api_key) == 0 {
				authErr = nebulaErrors.ErrUnauthorized
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
				return
			}

			// c.Set("databaseId", databaseId)

			// if bcrypt.CompareHashAndPassword([]byte(api_key), []byte(secretPart)) != nil {
			// 	authErr = nebulaErrors.ErrUnauthorized
			// 	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key format"})
			// 	return
			// }
			isApiKeyAuth = true
			c.Set("isApiKey", isApiKeyAuth)

		// --- End API Key Logic ---

		case "bearer":
			customLog.Println("CombinedAuthMiddleware: Attempting Bearer token authentication...")
			// --- JWT Validation Logic (adapted from AuthMiddleware) ---
			var jwtUserID string

			jwtUserID, authErr = nebulaErrors.ValidateJWT(credentials, cfg.JWTSecret) // Use internal func
			if authErr != nil {
				customLog.Printf("AuthMiddleware: Token validation failed: %v", authErr)
				// Map internal auth errors to HTTP status (will be improved by error mw)
				var statusCode = http.StatusUnauthorized
				var errMsg = "Invalid token"
				if errors.Is(authErr, auth.ErrTokenMalformed) {
					errMsg = authErr.Error()
				} else if errors.Is(authErr, auth.ErrTokenExpired) {
					errMsg = authErr.Error()
				} // other specific errors can be checked here

				c.Error(authErr)
				c.AbortWithStatusJSON(statusCode, gin.H{"error": errMsg}) // Temporary
				return
			}

			userId = jwtUserID
			databaseId = nil // Explicitly set databaseID to nil for JWT/user scope

		// --- End JWT Logic ---

		default:
			// Unsupported authentication scheme
			authErr = fmt.Errorf("%w: unsupported scheme '%s'", nebulaErrors.ErrTokenMalformed, parts[0])
		}

		// --- Handle Authentication Result ---
		if authErr != nil {
			customLog.Warnf("CombinedAuthMiddleware: Authentication failed (Scheme: %s): %v", scheme, authErr)
			c.Error(authErr) // Attach the specific auth error (e.g., ErrUnauthorized, ErrTokenExpired)
			// Let the main ErrorHandler middleware map this to the correct response
			c.Abort() // Abort further processing, ErrorHandler will respond
			return
		}

		// --- Authentication Success ---
		customLog.Printf("CombinedAuthMiddleware: Auth success. UserID: %s, DatabaseID: %v (Scheme: %s)\n", userId, databaseId, scheme)
		c.Set("userId", userId)
		c.Set("databaseId", databaseId) // Will be int64 for DB-scoped ApiKey, nil for JWT

		c.Next() // Proceed to the next handler

	}
}
