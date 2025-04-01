package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// --- Password Utilities ---

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error generating bcrypt hash: %v", err)
		return "", err
	}
	return string(bytes), nil
}

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil && err != bcrypt.ErrMismatchedHashAndPassword {
		log.Printf("Unexpected error comparing password hash: %v", err)
	}
	return err == nil
}

// --- JWT Utilities ---

func generateJWT(userID int64) (string, error) {
	claims := CustomClaims{
		userID,
		jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "go-baas-mvp",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(jwtSecretKey)
	if err != nil {
		log.Printf("Error signing JWT for user %d: %v", userID, err)
		return "", err
	}
	return signedToken, nil
}

// --- Middleware ---

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Println("AuthMiddleware: Authorization header missing")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			log.Println("AuthMiddleware: Authorization header format invalid")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header format must be Bearer {token}"})
			return
		}
		tokenString := parts[1]
		claims := &CustomClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecretKey, nil
		})

		if err != nil {
			log.Printf("AuthMiddleware: Token parsing error: %v", err)
			if errors.Is(err, jwt.ErrTokenMalformed) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Malformed token"})
			} else if errors.Is(err, jwt.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenNotValidYet) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token is expired or not valid yet"})
			} else {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Couldn't handle this token: " + err.Error()})
			}
			return
		}
		if !token.Valid || claims.UserID <= 0 {
			log.Println("AuthMiddleware: Invalid token or claims")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}
		log.Printf("AuthMiddleware: Token validated successfully for UserID: %d", claims.UserID)
		c.Set("userID", claims.UserID)
		c.Next()
	}
}

// --- Handlers ---

func signupHandler(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Signup binding error: %v", err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		log.Printf("Failed to hash password during signup for email %s: %v", req.Email, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}
	sqlStatement := `INSERT INTO users (email, password_hash) VALUES (?, ?)`
	result, err := metaDB.ExecContext(c.Request.Context(), sqlStatement, req.Email, hashedPassword)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			log.Printf("Signup attempt with duplicate email: %s", req.Email)
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "Email address already registered"})
			return
		}
		log.Printf("Failed to insert user %s into database: %v", req.Email, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to register user"})
		return
	}
	userID, err := result.LastInsertId()
	if err == nil {
		log.Printf("Successfully registered user ID %d with email %s", userID, req.Email)
	}
	c.JSON(http.StatusCreated, gin.H{"message": "User registered successfully"})
}

func loginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Login binding error: %v", err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	sqlStatement := `SELECT id, email, password_hash, created_at FROM users WHERE email = ? LIMIT 1`
	var user User
	err := metaDB.QueryRowContext(c.Request.Context(), sqlStatement, req.Email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("Login attempt failed for email %s: user not found", req.Email)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
			return
		}
		log.Printf("Database error during login for email %s: %v", req.Email, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Login failed due to server error"})
		return
	}
	if !checkPasswordHash(req.Password, user.PasswordHash) {
		log.Printf("Login attempt failed for email %s: invalid password", req.Email)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}
	log.Printf("Password verified for user ID %d (%s). Generating JWT...", user.ID, user.Email)
	tokenString, err := generateJWT(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate authentication token"})
		return
	}
	log.Printf("JWT generated successfully for user ID %d", user.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   tokenString,
	})
}
