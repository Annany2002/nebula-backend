package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubOAuthConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing ID", "GITHUB_OAUTH_CLIENT_ID", ""}, {"missing secret", "GITHUB_OAUTH_CLIENT_SECRET", ""},
		{"secret newline", "GITHUB_OAUTH_CLIENT_SECRET", "credential\r\n"}, {"invalid boolean", "GITHUB_OAUTH_ENABLED", "sometimes"},
		{"remote plaintext", "GITHUB_OAUTH_REDIRECT_URL", "http://app.example.test/auth/github/callback"},
		{"credentials", "GITHUB_OAUTH_REDIRECT_URL", "https://user:pass@app.example.test/auth/github/callback"},
		{"query", "GITHUB_OAUTH_REDIRECT_URL", "https://app.example.test/auth/github/callback?x=1"},
		{"fragment", "GITHUB_OAUTH_REDIRECT_URL", "https://app.example.test/auth/github/callback#x"},
		{"empty fragment", "GITHUB_OAUTH_REDIRECT_URL", "https://app.example.test/auth/github/callback#"},
		{"wrong path", "GITHUB_OAUTH_REDIRECT_URL", "https://app.example.test/sign-in"},
		{"invalid port", "GITHUB_OAUTH_REDIRECT_URL", "https://app.example.test:0/auth/github/callback"},
		{"escaped path", "GITHUB_OAUTH_REDIRECT_URL", "https://app.example.test/auth/github/%63allback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range map[string]string{"GITHUB_OAUTH_ENABLED": "true", "GITHUB_OAUTH_CLIENT_ID": "fixture-client", "GITHUB_OAUTH_CLIENT_SECRET": "fixture-secret", "GITHUB_OAUTH_REDIRECT_URL": "https://app.example.test/auth/github/callback"} {
				t.Setenv(key, value)
			}
			t.Setenv(tc.key, tc.value)
			_, err := loadGitHubConfig()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "fixture-secret")
		})
	}
	for _, callback := range []string{"https://app.example.test/auth/github/callback", "http://localhost:3000/auth/github/callback", "http://127.0.0.1:3000/auth/github/callback", "http://[::1]:3000/auth/github/callback"} {
		t.Run(callback, func(t *testing.T) {
			for key, value := range map[string]string{"GITHUB_OAUTH_ENABLED": "true", "GITHUB_OAUTH_CLIENT_ID": "fixture", "GITHUB_OAUTH_CLIENT_SECRET": "fixture-secret", "GITHUB_OAUTH_REDIRECT_URL": callback} {
				t.Setenv(key, value)
			}
			cfg, err := loadGitHubConfig()
			require.NoError(t, err)
			require.True(t, cfg.Enabled)
			require.Equal(t, callback, cfg.RedirectURL)
		})
	}
	t.Setenv("GITHUB_OAUTH_ENABLED", "false")
	t.Setenv("GITHUB_OAUTH_CLIENT_SECRET", "")
	t.Setenv("GITHUB_OAUTH_REDIRECT_URL", "broken")
	cfg, err := loadGitHubConfig()
	require.NoError(t, err)
	require.Equal(t, GitHubConfig{}, cfg)
}
