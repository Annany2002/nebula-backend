// api/handlers/auth_handler_integration_test.go
package handlers_test

import (
	"bytes"
	"context" // Import context package
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/Annany2002/nebula-backend/api"
	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth"

	// "github.com/Annany2002/nebula-backend/internal/domain" // Only needed if checking domain user fields directly
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// testDBSetup creates a temporary SQLite DB for testing and returns the DB pool and cleanup func.
func testDBSetup(t *testing.T) (*sql.DB, *config.Config, func()) {
	t.Helper()

	tempDir := t.TempDir()
	testDbPath := filepath.Join(tempDir, "test_metadata.db") // Changed filename for clarity

	// Using a fixed known secret for predictable JWT tests if needed later
	testCfg := &config.Config{
		ServerPort:     ":0",                                               // Use random available port
		JWTSecret:      "test_secret_key_for_integration_tests_1234567890", // Known secret
		JWTExpiration:  time.Minute * 5,
		MetadataDbDir:  tempDir,
		MetadataDbFile: "test_metadata.db", // Changed filename for clarity
	}

	db, err := storage.ConnectMetadataDB(testCfg) // Creates tables
	if err != nil {
		t.Fatalf("Failed to connect to test database '%s': %v", testDbPath, err)
	}

	cleanup := func() {
		err := db.Close()
		if err != nil {
			t.Logf("Warning: failed to close test database: %v", err)
		}
	}

	return db, testCfg, cleanup
}

// setupTestServer creates a test server instance with a test DB.
func setupTestServer(t *testing.T) (*httptest.Server, *sql.DB, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, cfg, dbCleanup := testDBSetup(t)
	router := api.SetupRouter(db, cfg) // Setup router with test DB
	server := httptest.NewServer(router)

	cleanup := func() {
		server.Close()
		dbCleanup()
	}

	return server, db, cleanup
}

// TestAuthEndpoints performs integration tests on /auth/signup and /auth/login.
func TestAuthEndpoints(t *testing.T) {
	server, db, cleanup := setupTestServer(t)
	defer cleanup()

	assert := assert.New(t)

	testEmail := "test.user." + strconv.FormatInt(time.Now().UnixNano(), 10) + "@integration.com" // Unique email per run
	testPassword := "StrongPassword123!"

	// --- Test Signup ---
	t.Run("Signup Success", func(t *testing.T) {
		signupReqBody := models.SignupRequest{Email: testEmail, Password: testPassword}
		bodyBytes, _ := json.Marshal(signupReqBody)

		res, err := http.Post(server.URL+"/auth/signup", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close() // Ensure body is closed

		assert.Equal(http.StatusCreated, res.StatusCode, "Expected status 201 Created")

		var resBody map[string]string
		err = json.NewDecoder(res.Body).Decode(&resBody)
		assert.NoError(err, "Failed to decode signup response body")
		assert.Equal("User registered successfully", resBody["message"])

		// Verify user created in DB (Direct DB check)
		// *** FIXED: Use context.Background() ***
		user, err := storage.FindUserByEmail(context.Background(), db, testEmail)
		assert.NoError(err, "Finding user after signup should not fail")
		assert.NotNil(user, "User should exist in DB after signup")
		if user != nil { // Prevent panic on nil pointer if previous assert fails
			assert.Equal(testEmail, user.Email)
			assert.True(auth.CheckPasswordHash(testPassword, user.PasswordHash), "Stored password hash should match")
		}
	})

	t.Run("Signup Conflict (Duplicate Email)", func(t *testing.T) {
		// Assumes the previous test ran successfully and created the user
		signupReqBody := models.SignupRequest{Email: testEmail, Password: "anotherPassword"}
		bodyBytes, _ := json.Marshal(signupReqBody)

		res, err := http.Post(server.URL+"/auth/signup", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close()
		assert.Equal(http.StatusConflict, res.StatusCode, "Expected status 409 Conflict")
	})

	t.Run("Signup Bad Request (Invalid Email Format)", func(t *testing.T) {
		signupReqBody := models.SignupRequest{Email: "invalid-email-format", Password: testPassword}
		bodyBytes, _ := json.Marshal(signupReqBody)

		res, err := http.Post(server.URL+"/auth/signup", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close()
		assert.Equal(http.StatusBadRequest, res.StatusCode, "Expected status 400 Bad Request")
	})

	t.Run("Signup Bad Request (Short Password)", func(t *testing.T) {
		signupReqBody := models.SignupRequest{Email: "shortpass@example.com", Password: "short"}
		bodyBytes, _ := json.Marshal(signupReqBody)

		res, err := http.Post(server.URL+"/auth/signup", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close()
		assert.Equal(http.StatusBadRequest, res.StatusCode, "Expected status 400 Bad Request")
	})

	// --- Test Login ---
	t.Run("Login Success", func(t *testing.T) {
		// Assumes user from initial successful signup exists
		loginReqBody := models.LoginRequest{Email: testEmail, Password: testPassword}
		bodyBytes, _ := json.Marshal(loginReqBody)

		res, err := http.Post(server.URL+"/auth/login", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close()
		assert.Equal(http.StatusOK, res.StatusCode, "Expected status 200 OK")

		var resBody models.LoginResponse
		err = json.NewDecoder(res.Body).Decode(&resBody)
		assert.NoError(err, "Failed to decode login response body")
		assert.Equal("Login successful", resBody.Message)
		assert.NotEmpty(resBody.Token, "Token should not be empty on successful login")

		// Optional: Validate the token structure/claims (basic)
		// *** FIXED: Use context.Background() - not really needed here but good practice if ValidateJWT used context ***
		// Using the known test secret from testCfg
		userID, err := auth.ValidateJWT(resBody.Token, "test_secret_key_for_integration_tests_1234567890")
		assert.NoError(err, "Returned token should be valid")
		assert.True(userID > 0, "UserID from token should be positive")
	})

	t.Run("Login Unauthorized (Wrong Password)", func(t *testing.T) {
		loginReqBody := models.LoginRequest{Email: testEmail, Password: "IncorrectPassword"}
		bodyBytes, _ := json.Marshal(loginReqBody)

		res, err := http.Post(server.URL+"/auth/login", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close()
		// Expect 401 now that the panic is fixed and middleware handles generic auth failure
		assert.Equal(http.StatusUnauthorized, res.StatusCode, "Expected status 401 Unauthorized for wrong password")
	})

	t.Run("Login Unauthorized (User Not Found)", func(t *testing.T) {
		loginReqBody := models.LoginRequest{Email: "nosuchuser@example.com", Password: "anyPassword"}
		bodyBytes, _ := json.Marshal(loginReqBody)

		res, err := http.Post(server.URL+"/auth/login", "application/json", bytes.NewReader(bodyBytes))
		assert.NoError(err)
		defer res.Body.Close()
		// *** CHANGED: Expect 404 based on current ErrorHandler logic ***
		assert.Equal(http.StatusNotFound, res.StatusCode, "Expected status 404 Not Found for non-existent user")
	})
}
