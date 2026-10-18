package githubauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

type transportFunc func(*http.Request) (*http.Response, error)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestProviderPreservesCancellationDuringResponseBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewProvider(config.GitHubConfig{}).(*githubProvider)
	p.client.Transport = transportFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(readerFunc(func(_ []byte) (int, error) {
			cancel()
			return 0, context.Canceled
		}))}, nil
	})
	_, err := p.Exchange(ctx, "code", "verifier")
	require.ErrorIs(t, err, context.Canceled)
}

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func providerFixture(t *testing.T, change func(*http.Request) *http.Response) Provider {
	t.Helper()
	settings := config.GitHubConfig{Enabled: true, ClientID: "fixture-id", ClientSecret: "fixture-secret", RedirectURL: "https://app.example.test/auth/github/callback"}
	p := NewProvider(settings).(*githubProvider)
	p.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "https", req.URL.Scheme)
		if change != nil {
			if res := change(req); res != nil {
				return res, nil
			}
		}
		switch req.URL.Path {
		case "/login/oauth/access_token":
			require.Equal(t, "github.com", req.URL.Host)
			require.Equal(t, "POST", req.Method)
			require.NoError(t, req.ParseForm())
			require.Equal(t, "fixture-secret", req.PostForm.Get("client_secret"))
			require.Equal(t, "code", req.PostForm.Get("code"))
			require.Equal(t, "verifier", req.PostForm.Get("code_verifier"))
			require.Equal(t, settings.RedirectURL, req.PostForm.Get("redirect_uri"))
			require.Empty(t, req.Header.Get("Authorization"))
			return reply(`{"access_token":"gho_fixture","token_type":"bearer","scope":"user:email"}`, 200), nil
		case "/user":
			require.Equal(t, "api.github.com", req.URL.Host)
			require.Equal(t, "Bearer gho_fixture", req.Header.Get("Authorization"))
			return reply(`{"id":12345,"login":"octocat","type":"User","email":"untrusted-public@example.test"}`, 200), nil
		case "/user/emails":
			return reply(`[{"email":"primary@example.test","primary":true,"verified":true}]`, 200), nil
		default:
			t.Fatalf("Unexpected provider endpoint %s", req.URL.Path)
			return nil, errors.New("unexpected endpoint")
		}
	})
	return p
}

func TestProviderUsesVerifiedPrimaryAndPKCE(t *testing.T) {
	p := providerFixture(t, nil)
	profile, err := p.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, Profile{Subject: "12345", Login: "octocat", Email: "primary@example.test"}, profile)
}

func TestProviderFailsClosedWithoutLeakingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
		want             error
	}{
		{"code rejected", "/login/oauth/access_token", `{"error":"bad_verification_code","error_description":"secret-upstream"}`, 200, ErrProviderCode},
		{"config rejected", "/login/oauth/access_token", `{"error":"incorrect_client_credentials"}`, 200, ErrProvider},
		{"wrong token type", "/login/oauth/access_token", `{"access_token":"gho_fixture","token_type":"other"}`, 200, ErrProvider},
		{"header injection", "/login/oauth/access_token", `{"access_token":"gho_fixture\r\nBad: header","token_type":"bearer"}`, 200, ErrProvider},
		{"missing token", "/login/oauth/access_token", `{}`, 200, ErrProvider},
		{"huge body", "/user", strings.Repeat("x", 128*1024+1), 200, ErrProvider},
		{"trailing JSON", "/user", `{"id":12345} {}`, 200, ErrProvider},
		{"non-json", "/user", "upstream-secret", 200, ErrProvider},
		{"wrong identity", "/user", `{"id":0,"login":"octocat","type":"User"}`, 200, ErrProvider},
		{"organization", "/user", `{"id":12345,"login":"octocat","type":"Organization"}`, 200, ErrProvider},
		{"unsafe login", "/user", `{"id":12345,"login":"<script>","type":"User"}`, 200, ErrProvider},
		{"missing email", "/user/emails", `[]`, 200, ErrVerifiedEmail},
		{"unverified primary", "/user/emails", `[{"email":"p@example.test","primary":true,"verified":false}]`, 200, ErrVerifiedEmail},
		{"nonprimary verified", "/user/emails", `[{"email":"p@example.test","primary":false,"verified":true}]`, 200, ErrVerifiedEmail},
		{"ambiguous primary", "/user/emails", `[{"email":"p@example.test","primary":true,"verified":true},{"email":"q@example.test","primary":true,"verified":true}]`, 200, ErrVerifiedEmail},
		{"redirect", "/user", "", 302, ErrProvider}, {"unavailable", "/user", "secret-upstream", 503, ErrProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := providerFixture(t, func(req *http.Request) *http.Response {
				if req.URL.Path == tc.path {
					return reply(tc.body, tc.status)
				}
				return nil
			})
			_, err := p.Exchange(context.Background(), "code", "verifier")
			require.ErrorIs(t, err, tc.want)
			require.NotContains(t, err.Error(), "secret-upstream")
			require.NotContains(t, err.Error(), "fixture-secret")
		})
	}
}

func TestProviderPaginatesEmailAddressesWithBoundedWork(t *testing.T) {
	first := make([]map[string]any, 100)
	for i := range first {
		first[i] = map[string]any{"email": "secondary@example.test", "primary": false, "verified": true}
	}
	body, err := json.Marshal(first)
	require.NoError(t, err)
	pages := 0
	p := providerFixture(t, func(req *http.Request) *http.Response {
		if req.URL.Path != "/user/emails" {
			return nil
		}
		pages++
		require.Equal(t, "100", req.URL.Query().Get("per_page"))
		if req.URL.Query().Get("page") == "1" {
			return reply(string(body), 200)
		}
		return reply(`[{"email":"private-primary@example.test","primary":true,"verified":true}]`, 200)
	})
	profile, err := p.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, "private-primary@example.test", profile.Email)
	require.Equal(t, 2, pages)
	pages = 0
	p = providerFixture(t, func(req *http.Request) *http.Response {
		if req.URL.Path == "/user/emails" {
			pages++
			return reply(string(body), 200)
		}
		return nil
	})
	_, err = p.Exchange(context.Background(), "code", "verifier")
	require.ErrorIs(t, err, ErrVerifiedEmail)
	require.Equal(t, 10, pages)
}

func TestProviderDoesNotFollowRedirectsOrRetryTransportErrors(t *testing.T) {
	p := NewProvider(config.GitHubConfig{}).(*githubProvider)
	require.ErrorIs(t, p.client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
	count := 0
	p.client.Transport = transportFunc(func(_ *http.Request) (*http.Response, error) {
		count++
		return nil, errors.New("secret transport detail")
	})
	_, err := p.Exchange(context.Background(), "code", "verifier")
	require.ErrorIs(t, err, ErrProvider)
	require.Equal(t, 1, count)
	require.NotContains(t, err.Error(), "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Exchange(ctx, "code", "verifier")
	require.ErrorIs(t, err, context.Canceled)
}
