// internal/auth/auth.go
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5" // Use v5 or adjust if using v4
	"golang.org/x/crypto/bcrypt"

	"github.com/Annany2002/nebula-backend/api/models" // Import DTO for CustomClaims
	"github.com/Annany2002/nebula-backend/internal/logger"
)

var (
	ErrBadRequest              = errors.New("bad request")
	ErrTokenMalformed          = errors.New("malformed token")
	ErrTokenExpired            = errors.New("token is expired or not valid yet")
	ErrTokenInvalid            = errors.New("invalid token")
	ErrTokenClaimsInvalid      = errors.New("invalid token claims")
	ErrUnauthorized            = errors.New("unauthorized")
	ErrInternalServer          = errors.New("authorization error")
	ErrForbidden               = errors.New("invalid api key")
	ErrUnexpectedSigningMethod = errors.New("unexpected token signing method")
	customLog                  = logger.NewLogger()
)

// --- Password Utilities ---

// HashPassword generates a bcrypt hash for the given password
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		customLog.Warnf("Error generating bcrypt hash: %v", err)
		// Don't return raw bcrypt error to caller usually
		return "", fmt.Errorf("failed to hash password")
	}
	return string(bytes), nil
}

// CheckPasswordHash compares a plaintext password with a stored bcrypt hash
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	// Log unexpected errors, but return false for mismatch or other errors
	if err != nil && !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		customLog.Warnf("Unexpected error comparing password hash: %v", err)
	}
	return err == nil
}

// --- JWT Utilities ---

// GenerateJWT creates a signed JWT string for a given userID
func GenerateJWT(userID, jwtSecret string, jwtExpiration time.Duration) (string, error) {
	// Set custom and standard claims
	claims := models.CustomClaims{ // Using the DTO struct from api/models
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "nebula-backend", // Consider making this configurable
		},
	}

	// Create token with claims and specified signing method
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign the token with our secret key
	signedToken, err := token.SignedString([]byte(jwtSecret)) // Convert secret string to byte slice
	if err != nil {
		customLog.Warnf("Error signing JWT for user %s: %v", userID, err)
		return "", fmt.Errorf("failed to generate token") // Generic error
	}

	return signedToken, nil
}

// ValidateJWT parses and validates a JWT string, returning the UserID if valid.
func ValidateJWT(tokenString, jwtSecret string) (string, error) {
	claims := &models.CustomClaims{} // Use pointer to the DTO struct

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Check the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			customLog.Warnf("ValidateJWT: Unexpected signing method: %v", token.Header["alg"])
			// Use wrapped error defined above
			return nil, fmt.Errorf("%w: %v", ErrUnexpectedSigningMethod, token.Header["alg"])
		}
		// Return the secret key for validation
		return []byte(jwtSecret), nil
	})

	// Handle parsing errors, mapping library errors to our defined errors
	if err != nil {
		customLog.Warnf("ValidateJWT: Token parsing error: %v", err)
		switch {
		case errors.Is(err, jwt.ErrTokenMalformed):
			return "", ErrTokenMalformed
		case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenNotValidYet):
			return "", ErrTokenExpired
		case errors.Is(err, ErrUnexpectedSigningMethod):
			return "", err
		default:
			return "", ErrTokenInvalid
		}
	}

	// Check if the token and claims are valid overall
	if !token.Valid {
		customLog.Warnf("ValidateJWT: Invalid token marked by library")
		return "", ErrTokenInvalid
	}

	// Check if userID is present in claims (should be, based on our generation logic)
	if claims.UserID == "" {
		customLog.Warnf("ValidateJWT: UserID missing or invalid in token claims")
		return "", ErrTokenClaimsInvalid
	}

	// Token is valid! Return the UserID.
	return claims.UserID, nil
}
