package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api"
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/accounttoken"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/githubauth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

type fixtureGitHub struct {
	mu      sync.Mutex
	calls   int
	profile githubauth.Profile
	err     error
	during  func()
}

func (p *fixtureGitHub) Exchange(_ context.Context, _, _ string) (githubauth.Profile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.during != nil {
		p.during()
	}
	return p.profile, p.err
}
func githubFixture(t *testing.T) (*backendFixture, *fixtureGitHub) {
	t.Helper()
	f := newBackendFixture(t)
	f.cfg.GitHub = config.GitHubConfig{Enabled: true, ClientID: "fixture-client", ClientSecret: "fixture-client-secret", RedirectURL: "https://app.example.test/auth/github/callback"}
	p := &fixtureGitHub{profile: githubauth.Profile{Subject: "12345", Login: "octocat", Email: "github@example.test"}}
	f.router = api.SetupRouterWithAuthProviders(f.meta, f.cfg, &verificationSender{}, p)
	return f, p
}
func githubProof() (string, string) {
	v := strings.Repeat("x", 43)
	sum := sha256.Sum256([]byte(v))
	return v, base64.RawURLEncoding.EncodeToString(sum[:])
}
func githubStart(t *testing.T, f *backendFixture, linked bool) string {
	t.Helper()
	_, challenge := githubProof()
	path, authorization := "/auth/github/start", ""
	body := map[string]string{"code_challenge": challenge}
	if linked {
		path = "/api/v1/account/identities/github/start"
		authorization = "Bearer " + f.token
		body["current_password"] = "Password123!"
	}
	out := f.request("POST", path, authorization, fixtureJSON(t, body))
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	var response struct {
		URL     string `json:"authorization_url"`
		Expires int    `json:"expires_in"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
	require.Equal(t, 600, response.Expires)
	u, err := url.Parse(response.URL)
	require.NoError(t, err)
	require.Equal(t, "github.com", u.Host)
	require.Equal(t, "https", u.Scheme)
	require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
	require.Equal(t, challenge, u.Query().Get("code_challenge"))
	require.Equal(t, "user:email", u.Query().Get("scope"))
	require.Equal(t, f.cfg.GitHub.RedirectURL, u.Query().Get("redirect_uri"))
	require.NotContains(t, response.URL, "fixture-client-secret")
	state := u.Query().Get("state")
	require.True(t, accounttoken.Valid(state))
	var digest string
	require.NoError(t, f.meta.QueryRow(`SELECT state_hash FROM github_oauth_states WHERE state_hash=?`, accounttoken.Digest(state)).Scan(&digest))
	require.NotEqual(t, state, digest)
	return state
}
func githubCallback(t *testing.T, f *backendFixture, state string) *httptest.ResponseRecorder {
	t.Helper()
	verifier, _ := githubProof()
	return f.request("POST", "/auth/github/callback", "", fixtureJSON(t, map[string]string{"code": "fixture-code", "state": state, "code_verifier": verifier}))
}

func TestGitHubSignInCreatesVerifiedAccountAndPreservesIdentity(t *testing.T) {
	f, p := githubFixture(t)
	f.cfg.Email.Enabled = true
	f.router = api.SetupRouterWithAuthProviders(f.meta, f.cfg, &verificationSender{}, p)
	state := githubStart(t, f, false)
	out := githubCallback(t, f, state)
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	var response struct {
		Token string `json:"token"`
		User  struct {
			ID       string `json:"userId"`
			Email    string `json:"email"`
			Verified bool   `json:"email_verified"`
			Required bool   `json:"verification_required"`
		} `json:"user"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
	require.NotEmpty(t, response.Token)
	require.True(t, response.User.Verified)
	require.True(t, response.User.Required)
	require.NotContains(t, out.Body.String(), "password_hash")
	require.NotContains(t, out.Body.String(), "fixture-client-secret")
	require.Equal(t, 200, f.request("GET", "/api/v1/account/user/me", "Bearer "+response.Token, "").Code)
	out = f.request("POST", "/auth/login", "", `{"email":"github@example.test","password":"Password123!"}`)
	require.Equal(t, 401, out.Code)
	require.Equal(t, 400, githubCallback(t, f, state).Code)
	require.Equal(t, 1, p.calls)
	p.profile.Email = "changed@example.test"
	out = githubCallback(t, f, githubStart(t, f, false))
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), response.User.ID)
	require.Contains(t, out.Body.String(), "github@example.test")
	require.NotContains(t, out.Body.String(), "changed@example.test")
	_, err := f.meta.Exec(`INSERT INTO account_suspensions(user_id) VALUES(?)`, response.User.ID)
	require.NoError(t, err)
	out = githubCallback(t, f, githubStart(t, f, false))
	require.Equal(t, 403, out.Code)
	require.Contains(t, out.Body.String(), "github_account_unavailable")
}

func TestGitHubStateProofExpiryAndUpstreamFailure(t *testing.T) {
	for _, mode := range []string{"wrong proof", "expired", "unknown", "upstream", "unverified email", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			f, p := githubFixture(t)
			state := githubStart(t, f, false)
			verifier, _ := githubProof()
			status := 400
			switch mode {
			case "wrong proof":
				verifier = strings.Repeat("y", 43)
			case "expired":
				_, err := f.meta.Exec(`UPDATE github_oauth_states SET expires_at=?`, time.Now().Unix()-1)
				require.NoError(t, err)
			case "unknown":
				state = strings.Repeat("A", 43)
			case "upstream":
				p.err = errors.New("private upstream detail")
				status = 503
			case "unverified email":
				p.err = githubauth.ErrVerifiedEmail
				status = 403
			case "cancelled":
				p.err = context.Canceled
				status = 408
			}
			out := f.request("POST", "/auth/github/callback", "", fixtureJSON(t, map[string]string{"code": "fixture-code", "state": state, "code_verifier": verifier}))
			require.Equal(t, status, out.Code, out.Body.String())
			require.NotContains(t, out.Body.String(), "private upstream")
			if mode == "wrong proof" {
				require.Equal(t, 0, p.calls)
				require.Equal(t, 200, githubCallback(t, f, state).Code)
			}
			if mode == "upstream" || mode == "unverified email" || mode == "cancelled" {
				require.Equal(t, 400, githubCallback(t, f, state).Code)
				require.Equal(t, 1, p.calls)
			}
		})
	}
}

func TestGitHubRequiresExplicitLinkAndPasswordReauthentication(t *testing.T) {
	f, p := githubFixture(t)
	p.profile.Email = "REGRESSION@example.test"
	databaseID, _ := f.database(t, "preserved")
	key, keyErr := storage.StoreAPIKey(context.Background(), f.meta, "regression-owner", databaseID)
	require.NoError(t, keyErr)
	out := githubCallback(t, f, githubStart(t, f, false))
	require.Equal(t, 409, out.Code)
	require.Contains(t, out.Body.String(), "github_account_conflict")
	hash, err := auth.HashPassword("Password123!")
	require.NoError(t, err)
	_, err = f.meta.Exec(`UPDATE users SET password_hash=? WHERE user_id='regression-owner'`, hash)
	require.NoError(t, err)
	_, challenge := githubProof()
	out = f.request("POST", "/api/v1/account/identities/github/start", "", fixtureJSON(t, map[string]string{"code_challenge": challenge, "current_password": "Password123!"}))
	require.Equal(t, 401, out.Code)
	out = f.request("POST", "/api/v1/account/identities/github/start", "Bearer "+f.token, fixtureJSON(t, map[string]string{"code_challenge": challenge, "current_password": "wrong"}))
	require.Equal(t, 401, out.Code)
	require.Contains(t, out.Body.String(), "reauthentication_required")
	state := githubStart(t, f, true)
	out = githubCallback(t, f, state)
	require.Equal(t, 200, out.Code, out.Body.String())
	require.NotContains(t, out.Body.String(), "token")
	require.Contains(t, out.Body.String(), "linked")
	require.Equal(t, 200, f.request("GET", "/api/v1/databases/preserved/tables", "ApiKey "+key, "").Code)
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+f.token, "")
	require.Equal(t, 401, out.Code)
	require.Contains(t, out.Body.String(), "session_revoked")
	out = githubCallback(t, f, githubStart(t, f, false))
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), "regression-owner")
	require.Contains(t, out.Body.String(), "regression@example.test")
}

func TestGitHubLinkRechecksSessionAfterProviderExchange(t *testing.T) {
	f, p := githubFixture(t)
	hash, err := auth.HashPassword("Password123!")
	require.NoError(t, err)
	_, err = f.meta.Exec(`UPDATE users SET password_hash=? WHERE user_id='regression-owner'`, hash)
	require.NoError(t, err)
	state := githubStart(t, f, true)
	p.during = func() {
		_, err := f.meta.Exec(`INSERT INTO account_email_verifications(user_id,session_version) VALUES('regression-owner',1)`)
		require.NoError(t, err)
	}
	out := githubCallback(t, f, state)
	require.Equal(t, 403, out.Code)
	var count int
	require.NoError(t, f.meta.QueryRow(`SELECT COUNT(*) FROM account_oauth_identities`).Scan(&count))
	require.Zero(t, count)
}

func TestGitHubJSONBoundaryAndDisabledConfiguration(t *testing.T) {
	for _, body := range []string{`{}`, `{"code_challenge":"short"}`, `{"code_challenge":"` + strings.Repeat("A", 43) + `","redirect_uri":"https://evil.example.test"}`, `{} {}`, strings.Repeat("x", 4097)} {
		f, p := githubFixture(t)
		out := f.request("POST", "/auth/github/start", "", body)
		require.Contains(t, []int{400, 413}, out.Code)
		require.Zero(t, p.calls)
	}
	f, _ := githubFixture(t)
	req := httptest.NewRequest("POST", "/auth/github/start", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 415, out.Code)
	f.cfg.GitHub.Enabled = false
	f.router = api.SetupRouter(f.meta, f.cfg)
	for _, path := range []string{"/auth/github/start", "/auth/github/callback"} {
		out = f.request("POST", path, "", `{}`)
		require.Equal(t, http.StatusServiceUnavailable, out.Code)
		require.Contains(t, out.Body.String(), "github_oauth_disabled")
	}
	require.Equal(t, 404, f.request("GET", "/auth/github/callback?code=unsafe", "", "").Code)
}

func TestGitHubConcurrentCallbacksExchangeOnlyOnce(t *testing.T) {
	f, p := githubFixture(t)
	state := githubStart(t, f, false)
	results := make(chan int, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			verifier, _ := githubProof()
			out := f.request("POST", "/auth/github/callback", "", fixtureJSON(t, map[string]string{"code": "fixture-code", "state": state, "code_verifier": verifier}))
			results <- out.Code
		}()
	}
	workers.Wait()
	close(results)
	success := 0
	for code := range results {
		if code == 200 {
			success++
		} else {
			require.Equal(t, 400, code)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, p.calls)
}

func TestGitHubStateQuotaAndTrustedProxyIsolation(t *testing.T) {
	f, _ := githubFixture(t)
	_, challenge := githubProof()
	start := func(remote, forwarded string) int {
		req := httptest.NewRequest("POST", "/auth/github/start", strings.NewReader(fixtureJSON(t, map[string]string{"code_challenge": challenge})))
		req.RemoteAddr = remote
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("CF-Connecting-IP", forwarded)
		out := httptest.NewRecorder()
		f.router.ServeHTTP(out, req)
		if out.Code == 429 {
			require.Equal(t, "600", out.Header().Get("Retry-After"))
		}
		return out.Code
	}
	for i := 0; i < 20; i++ {
		require.Equal(t, 200, start("127.0.0.1:1234", "198.51.100.1"))
	}
	require.Equal(t, 429, start("127.0.0.1:1234", "198.51.100.1"))
	require.Equal(t, 200, start("127.0.0.1:1234", "198.51.100.2"))
	// Direct remote clients cannot move their quota by forging the connector header.
	for i := 0; i < 20; i++ {
		require.Equal(t, 200, start("192.0.2.1:1234", fmt.Sprintf("203.0.113.%d", i)))
	}
	require.Equal(t, 429, start("192.0.2.1:1234", "203.0.113.99"))
}

func TestAuthProviderDiscoveryExposesOnlyEnabledMethods(t *testing.T) {
	f, _ := githubFixture(t)
	out := f.request("GET", "/auth/providers", "", "")
	require.Equal(t, 200, out.Code)
	require.JSONEq(t, `{"providers":{"password":true,"github":true}}`, out.Body.String())
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.NotContains(t, out.Body.String(), "fixture-client")
	f.cfg.GitHub.Enabled = false
	f.router = api.SetupRouter(f.meta, f.cfg)
	out = f.request("GET", "/auth/providers", "", "")
	require.JSONEq(t, `{"providers":{"password":true,"github":false}}`, out.Body.String())
}
