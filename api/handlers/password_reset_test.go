package handlers_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api"
	"github.com/Annany2002/nebula-backend/internal/accounttoken"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func recoveryFixture(t *testing.T) (*backendFixture, *verificationSender) {
	t.Helper()
	f := newBackendFixture(t)
	f.cfg.Email.PasswordResetEnabled = true
	f.cfg.Email.PasswordResetTTL = 30 * time.Minute
	f.cfg.Email.AppURL = "https://app.example.test"
	hash, err := auth.HashPassword("Password123!")
	require.NoError(t, err)
	_, err = f.meta.Exec(`UPDATE users SET password_hash=? WHERE user_id='regression-owner'`, hash)
	require.NoError(t, err)
	sender := &verificationSender{}
	f.router = api.SetupRouterWithEmailSender(f.meta, f.cfg, sender)
	return f, sender
}

func recoveryToken(t *testing.T, sender *verificationSender) string {
	t.Helper()
	var body string
	require.Eventually(t, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		for _, message := range sender.messages {
			if message.subject == "Reset your Nebula password" {
				body = message.body
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
	start := strings.Index(body, "https://app.example.test/reset-password#")
	require.NotEqual(t, -1, start)
	u, err := url.Parse(strings.SplitN(body[start:], "\n", 2)[0])
	require.NoError(t, err)
	require.Empty(t, u.RawQuery)
	fragment, err := url.ParseQuery(u.Fragment)
	require.NoError(t, err)
	return fragment.Get("token")
}

func TestPasswordRecoveryEndToEndPreservesDataAndRequiresFreshLogin(t *testing.T) {
	f, sender := recoveryFixture(t)
	id, db := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	known := f.request("POST", "/auth/forgot-password", "", `{"email":"regression@example.test"}`)
	unknown := f.request("POST", "/auth/forgot-password", "", `{"email":"missing@example.test"}`)
	require.Equal(t, 202, known.Code)
	require.Equal(t, known.Code, unknown.Code)
	require.Equal(t, known.Body.String(), unknown.Body.String())
	require.Equal(t, "no-store", known.Header().Get("Cache-Control"))
	token := recoveryToken(t, sender)
	require.NotContains(t, known.Body.String(), token)
	var digest string
	require.NoError(t, f.meta.QueryRow(`SELECT token_hash FROM account_password_reset_tokens`).Scan(&digest))
	require.Equal(t, accounttoken.Digest(token), digest)
	// Neither reading a link nor requesting recovery changes the password or session.
	out := f.request("GET", "/auth/reset-password", "", "")
	require.Equal(t, 404, out.Code)
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code)
	out = f.request("POST", "/auth/reset-password", "", fixtureJSON(t, map[string]string{"token": token, "password": "NewPassword123!"}))
	require.Equal(t, 200, out.Code, out.Body.String())
	require.NotContains(t, out.Body.String(), "token")
	out = f.request("POST", "/auth/reset-password", "", fixtureJSON(t, map[string]string{"token": token, "password": "AnotherPassword123!"}))
	require.Equal(t, 400, out.Code)
	out = f.request("POST", "/auth/login", "", `{"email":"regression@example.test","password":"Password123!"}`)
	require.Equal(t, 401, out.Code)
	out = f.request("POST", "/auth/login", "", `{"email":"regression@example.test","password":"NewPassword123!"}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	var login struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &login))
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+f.token, "")
	require.Equal(t, 401, out.Code)
	require.Contains(t, out.Body.String(), "session_revoked")
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+login.Token, "")
	require.Equal(t, 200, out.Code)
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+key, "")
	require.Equal(t, 200, out.Code)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count))
	require.Equal(t, 1, count)
	require.Eventually(t, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		for _, m := range sender.messages {
			if m.subject == "Your Nebula password changed" {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}

func TestPasswordRecoveryCannotBypassVerificationOrSuspension(t *testing.T) {
	for _, mode := range []string{"verification", "suspended"} {
		t.Run(mode, func(t *testing.T) {
			f, sender := recoveryFixture(t)
			if mode == "verification" {
				_, err := f.meta.Exec(`INSERT INTO account_email_verifications(user_id,required) VALUES('regression-owner',1)`)
				require.NoError(t, err)
			}
			out := f.request("POST", "/auth/forgot-password", "", `{"email":"regression@example.test"}`)
			require.Equal(t, 202, out.Code)
			token := recoveryToken(t, sender)
			if mode == "suspended" {
				_, err := f.meta.Exec(`INSERT INTO account_suspensions(user_id) VALUES('regression-owner')`)
				require.NoError(t, err)
			}
			out = f.request("POST", "/auth/reset-password", "", fixtureJSON(t, map[string]string{"token": token, "password": "NewPassword123!"}))
			if mode == "suspended" {
				require.Equal(t, 400, out.Code)
			} else {
				require.Equal(t, 200, out.Code)
				out = f.request("POST", "/auth/login", "", `{"email":"regression@example.test","password":"NewPassword123!"}`)
				require.Equal(t, 403, out.Code)
				require.Contains(t, out.Body.String(), "email_verification_required")
			}
		})
	}
}

func TestPasswordRecoveryRequestValidationAndDisabledConfiguration(t *testing.T) {
	f, _ := recoveryFixture(t)
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/auth/forgot-password", `{"email":"invalid"}`, 400},
		{"/auth/forgot-password", `{"email":"owner@example.test","extra":true}`, 400},
		{"/auth/forgot-password", `null`, 400},
		{"/auth/reset-password", `{"token":"bad","password":"NewPassword123!"}`, 400},
		{"/auth/reset-password", `{"token":"bad","password":"short"}`, 400},
		{"/auth/reset-password", fixtureJSON(t, map[string]string{"token": "bad", "password": strings.Repeat("é", 37)}), 400},
		{"/auth/reset-password", `{"token":"bad","password":"NewPassword123!"}` + strings.Repeat(" ", 4096), 413},
	} {
		// Reset the shared IP budget so each assertion checks validation, not its preceding requests.
		_, err := f.meta.Exec(`DELETE FROM email_request_limits`)
		require.NoError(t, err)
		out := f.request("POST", tc.path, "", tc.body)
		require.Equal(t, tc.status, out.Code, out.Body.String())
	}
	request := httptest.NewRequest("POST", "/auth/reset-password", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	require.Equal(t, 415, response.Code)
	f.cfg.Email.PasswordResetEnabled = false
	out := f.request("POST", "/auth/forgot-password", "", `{"email":"owner@example.test"}`)
	require.Equal(t, 503, out.Code)
	require.Contains(t, out.Body.String(), "password_reset_disabled")
	out = f.request("POST", "/auth/reset-password", "", `{"token":"bad","password":"NewPassword123!"}`)
	require.Equal(t, 503, out.Code)
}
