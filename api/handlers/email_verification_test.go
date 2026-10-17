package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

type verificationMail struct{ to, subject, body string }
type verificationSender struct {
	mu       sync.Mutex
	messages []verificationMail
	failure  bool
}

func (s *verificationSender) Send(_ context.Context, to, subject, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure {
		return errors.New("fixture SMTP failure")
	}
	s.messages = append(s.messages, verificationMail{to, subject, body})
	return nil
}
func (s *verificationSender) token(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.messages)
	body := s.messages[len(s.messages)-1].body
	start := strings.Index(body, "https://app.example.test/verify-email#")
	require.NotEqual(t, -1, start)
	line := strings.SplitN(body[start:], "\n", 2)[0]
	link, err := url.Parse(line)
	require.NoError(t, err)
	require.Empty(t, link.RawQuery)
	query, err := url.ParseQuery(link.Fragment)
	require.NoError(t, err)
	return query.Get("token")
}
func emailFixture(t *testing.T) (*backendFixture, *verificationSender) {
	f := newBackendFixture(t)
	f.cfg.Email.Enabled = true
	f.cfg.Email.AppURL = "https://app.example.test"
	f.cfg.Email.TokenTTL = 30 * time.Minute
	sender := &verificationSender{}
	f.router = api.SetupRouterWithEmailSender(f.meta, f.cfg, sender)
	return f, sender
}

func TestEmailVerificationSignupLoginAndProtectedRoutes(t *testing.T) {
	f, sender := emailFixture(t)
	out := f.request("POST", "/auth/signup", "", `{"username":"newowner","email":"owner@example.test","password":"Password123!"}`)
	require.Equal(t, 201, out.Code, out.Body.String())
	require.Contains(t, out.Body.String(), `"verification_required":true`)
	token := sender.token(t)
	require.NotContains(t, out.Body.String(), token)
	user, err := storage.FindUserByEmail(context.Background(), f.meta, "owner@example.test")
	require.NoError(t, err)
	var digest string
	require.NoError(t, f.meta.QueryRow(`SELECT token_hash FROM account_email_tokens WHERE user_id=?`, user.UserId).Scan(&digest))
	sum := sha256.Sum256([]byte(token))
	require.Equal(t, hex.EncodeToString(sum[:]), digest)
	require.NotEqual(t, token, digest)
	out = f.request("POST", "/auth/login", "", `{"email":"owner@example.test","password":"Password123!"}`)
	require.Equal(t, 403, out.Code)
	require.Contains(t, out.Body.String(), "email_verification_required")
	jwt, err := auth.GenerateJWT(user.UserId, f.cfg.JWTSecret, time.Hour)
	require.NoError(t, err)
	for _, path := range []string{"/api/v1/databases", "/api/v1/backups", "/api/v1/account/user/me"} {
		out = f.request("GET", path, "Bearer "+jwt, "")
		require.Equal(t, 403, out.Code, path)
	}
	out = f.request("POST", "/auth/verify-email", "", fixtureJSON(t, map[string]string{"token": token}))
	require.Equal(t, 200, out.Code, out.Body.String())
	require.NotContains(t, out.Body.String(), token)
	out = f.request("POST", "/auth/verify-email", "", fixtureJSON(t, map[string]string{"token": token}))
	require.Equal(t, 400, out.Code)
	out = f.request("POST", "/auth/login", "", `{"email":"owner@example.test","password":"Password123!"}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Contains(t, out.Body.String(), `"email_verified":true`)
	out = f.request("GET", "/api/v1/databases", "Bearer "+jwt, "")
	require.Equal(t, 200, out.Code)
	// Existing users keep access while honestly reporting their unverified legacy email.
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), `"email_verified":false`)
	require.Contains(t, out.Body.String(), `"verification_required":false`)
}

func TestEmailChangesConfirmAddressAndRevokeOldSessions(t *testing.T) {
	f, sender := emailFixture(t)
	databaseID, _ := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", databaseID)
	require.NoError(t, err)
	hash, err := auth.HashPassword("Password123!")
	require.NoError(t, err)
	_, err = f.meta.Exec(`UPDATE users SET password_hash=? WHERE user_id='regression-owner'`, hash)
	require.NoError(t, err)
	out := f.request("PUT", "/api/v1/account/user/me", "Bearer "+f.token, `{"email":"changed@example.test"}`)
	require.Equal(t, 401, out.Code)
	out = f.request("PUT", "/api/v1/account/user/me", "Bearer "+f.token, `{"email":"changed@example.test","current_password":"Password123!"}`)
	require.Equal(t, 202, out.Code, out.Body.String())
	user, err := storage.FindUserByUserId(context.Background(), f.meta, "regression-owner")
	require.NoError(t, err)
	require.Equal(t, "regression@example.test", user.Email)
	require.Equal(t, "changed@example.test", user.PendingEmail)
	out = f.request("PUT", "/api/v1/account/user/me", "Bearer "+f.token, `{"email":"changed@example.test","current_password":"Password123!"}`)
	require.Equal(t, 429, out.Code)
	retry, err := strconv.Atoi(out.Header().Get("Retry-After"))
	require.NoError(t, err)
	require.InDelta(t, 60, retry, 2)
	token := sender.token(t)
	out = f.request("POST", "/auth/verify-email", "", fixtureJSON(t, map[string]string{"token": token}))
	require.Equal(t, 200, out.Code, out.Body.String())
	for _, path := range []string{"/api/v1/account/user/me", "/api/v1/databases"} {
		out = f.request("GET", path, "Bearer "+f.token, "")
		require.Equal(t, 401, out.Code)
		require.Contains(t, out.Body.String(), "session_revoked")
	}
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+key, "")
	require.Equal(t, 200, out.Code, "Confirmed email changes preserve database API keys")
	out = f.request("POST", "/auth/login", "", `{"email":"changed@example.test","password":"Password123!"}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	var login struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &login))
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+login.Token, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Contains(t, out.Body.String(), `"email_verified":true`)
	f.cfg.Email.Enabled = false
	out = f.request("PUT", "/api/v1/account/user/me", "Bearer "+login.Token, `{"email":"bypass@example.test"}`)
	require.Equal(t, 503, out.Code)
}

func TestPendingEmailBlocksAPIKeyUntilVerified(t *testing.T) {
	f, sender := emailFixture(t)
	databaseID, _ := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", databaseID)
	require.NoError(t, err)
	_, err = f.meta.Exec(`INSERT INTO account_email_verifications(user_id,required) VALUES('regression-owner',1)`)
	require.NoError(t, err)
	out := f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+key, "")
	require.Equal(t, 403, out.Code)
	require.Contains(t, out.Body.String(), "email_verification_required")
	out = f.request("POST", "/auth/resend-verification", "", `{"email":"regression@example.test"}`)
	require.Equal(t, 202, out.Code)
	require.Eventually(t, func() bool { sender.mu.Lock(); defer sender.mu.Unlock(); return len(sender.messages) == 1 }, time.Second, 10*time.Millisecond)
	out = f.request("POST", "/auth/verify-email", "", fixtureJSON(t, map[string]string{"token": sender.token(t)}))
	require.Equal(t, 200, out.Code)
	out = f.request("GET", "/api/v1/databases/alpha/tables", "ApiKey "+key, "")
	require.Equal(t, 200, out.Code, out.Body.String())
}

func TestFailedSignupMailAndResendDoNotGrantAccess(t *testing.T) {
	f, sender := emailFixture(t)
	sender.failure = true
	out := f.request("POST", "/auth/signup", "", `{"username":"newowner","email":"owner@example.test","password":"Password123!"}`)
	require.Equal(t, 201, out.Code)
	require.Contains(t, out.Body.String(), `"verification_delivery":"unconfirmed"`)
	var tokens int
	require.NoError(t, f.meta.QueryRow(`SELECT COUNT(*) FROM account_email_tokens`).Scan(&tokens))
	require.Zero(t, tokens)
	out = f.request("POST", "/auth/login", "", `{"email":"owner@example.test","password":"Password123!"}`)
	require.Equal(t, 403, out.Code)
	sender.mu.Lock()
	sender.failure = false
	sender.mu.Unlock()
	_, err := f.meta.Exec(`UPDATE email_send_limits SET last_requested_at=0`)
	require.NoError(t, err)
	known := f.request("POST", "/auth/resend-verification", "", `{"email":"owner@example.test"}`)
	unknown := f.request("POST", "/auth/resend-verification", "", `{"email":"missing@example.test"}`)
	require.Equal(t, 202, known.Code)
	require.Equal(t, known.Code, unknown.Code)
	require.Equal(t, known.Body.String(), unknown.Body.String())
	require.Eventually(t, func() bool { sender.mu.Lock(); defer sender.mu.Unlock(); return len(sender.messages) == 1 }, time.Second, 10*time.Millisecond)
	token := sender.token(t)
	out = f.request("POST", "/auth/verify-email", "", fixtureJSON(t, map[string]string{"token": token}))
	require.Equal(t, 200, out.Code)
}

func TestVerificationRejectsMalformedOversizedAndExpiredTokens(t *testing.T) {
	f, sender := emailFixture(t)
	for _, body := range []string{`{}`, `{"token":"bad"}`, `{"token":"bad","extra":true}`, `{"token":"bad"} {}`, `null`} {
		out := f.request("POST", "/auth/verify-email", "", body)
		require.Equal(t, 400, out.Code, body)
	}
	out := f.request("POST", "/auth/verify-email", "", `{"token":"`+strings.Repeat("x", 5000)+`"}`)
	require.Equal(t, 413, out.Code)
	out = f.request("POST", "/auth/signup", "", `{"username":"newowner","email":"owner@example.test","password":"Password123!"}`)
	require.Equal(t, 201, out.Code)
	token := sender.token(t)
	_, err := f.meta.Exec(`UPDATE account_email_tokens SET expires_at=0`)
	require.NoError(t, err)
	out = f.request("POST", "/auth/verify-email", "", fixtureJSON(t, map[string]string{"token": token}))
	require.Equal(t, 400, out.Code)
	user, err := storage.FindUserByEmail(context.Background(), f.meta, "owner@example.test")
	require.NoError(t, err)
	require.False(t, user.EmailVerified)
	for i := 0; i < 9; i++ {
		out = f.request("POST", "/auth/resend-verification", "", `{"email":"missing@example.test"}`)
		require.Contains(t, []int{202, 503}, out.Code)
	}
	out = f.request("POST", "/auth/resend-verification", "", `{"email":"missing@example.test"}`)
	require.Equal(t, 429, out.Code)
	require.Equal(t, "900", out.Header().Get("Retry-After"))
}

func TestAccountBodiesRejectOversizedTailsAndTrailingJSON(t *testing.T) {
	f, _ := emailFixture(t)
	for _, route := range []struct{ method, path, auth, body string }{
		{"POST", "/auth/signup", "", `{"username":"newowner","email":"owner@example.test","password":"Password123!"}`},
		{"POST", "/auth/login", "", `{"email":"regression@example.test","password":"Password123!"}`},
		{"PUT", "/api/v1/account/user/me", "Bearer " + f.token, `{"username":"newowner"}`},
		{"POST", "/auth/verify-email", "", `{"token":"test"}`},
		{"POST", "/auth/resend-verification", "", `{"email":"owner@example.test"}`},
	} {
		t.Run(route.path, func(t *testing.T) {
			out := f.request(route.method, route.path, route.auth, route.body+strings.Repeat(" ", 4096))
			require.Equal(t, 413, out.Code, out.Body.String())
			out = f.request(route.method, route.path, route.auth, route.body+` {}`)
			require.Equal(t, 400, out.Code, out.Body.String())
		})
	}
}

func TestEmailRetryAfterIsExposedAcrossOrigins(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.test")
	f, _ := emailFixture(t)
	request := httptest.NewRequest("GET", "/health", http.NoBody)
	request.Header.Set("Origin", "https://app.example.test")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Header().Get("Access-Control-Expose-Headers"), "Retry-After")
}
