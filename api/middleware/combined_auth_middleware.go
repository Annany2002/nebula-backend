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
	"golang.org/x/crypto/bcrypt"
)

var (
	customLog    = logger.NewLogger()
	apiKeyPrefix = "neb_"
)

func CombinedAuthMiddleware(db *sql.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No Authorization header provided at all
			err := nebulaErrors.ErrUnauthorized // Use specific error
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
		var databaseId interface{} // Use interface{} to allow nil for JWT/user-key scope
		var authErr error

		// --- Try Different Authentication Schemes ---
		switch scheme {
		case "apikey":
			customLog.Println("CombinedAuthMiddleware: Attempting ApiKey authentication...")
			if !strings.HasPrefix(credentials, apiKeyPrefix) { // apiKeyPrefix defined in apikey_auth_middleware or storage
				authErr = fmt.Errorf("%w: invalid key prefix", nebulaErrors.ErrTokenMalformed)
				break // Go to error handling outside switch
			}
			keyPrefix := apiKeyPrefix
			secretPart := strings.TrimPrefix(credentials, keyPrefix)

			dbID, ok := databaseId.(int64)
			if !ok {
				authErr = fmt.Errorf("invalid databaseId type: expected int64, got %T", databaseId)
				break
			}

			api_key, err := storage.FindAPIKeyByDatabaseId(c.Request.Context(), db, dbID)
			if err != nil {
				customLog.Warnf("CombinedAuthMiddleware: DB error looking up ApiKey prefix '%s': %v", keyPrefix, err)
				authErr = fmt.Errorf("internal error during auth: %w", err) // Wrap internal DB error
				break
			}
			if len(api_key) == 0 {
				authErr = nebulaErrors.ErrUnauthorized // Use specific error; message set by error handler
				break
			}

			isValidKey := false
			if bcrypt.CompareHashAndPassword([]byte(api_key), []byte(secretPart)) == nil {
				isValidKey = true
				// Optional: Update last_used_at
				break
			}

			if !isValidKey {
				authErr = nebulaErrors.ErrUnauthorized
			}
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
