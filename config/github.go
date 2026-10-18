package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// GitHubConfig enables the provider independently of SMTP and password authentication.
type GitHubConfig struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func loadGitHubConfig() (GitHubConfig, error) {
	var result GitHubConfig
	enabled, err := strconv.ParseBool(getEnv("GITHUB_OAUTH_ENABLED", "false"))
	if err != nil {
		return result, errors.New("GITHUB_OAUTH_ENABLED must be a boolean")
	}
	if !enabled {
		return result, nil
	}
	result = GitHubConfig{Enabled: true, ClientID: os.Getenv("GITHUB_OAUTH_CLIENT_ID"), ClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"), RedirectURL: os.Getenv("GITHUB_OAUTH_REDIRECT_URL")}
	for _, value := range []string{result.ClientID, result.ClientSecret} {
		if value == "" || len(value) > 256 || strings.ContainsAny(value, " \t\r\n") {
			return GitHubConfig{}, errors.New("GitHub OAuth client ID and secret are required without whitespace")
		}
	}
	u, err := url.Parse(result.RedirectURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != "/auth/github/callback" || strings.ContainsAny(result.RedirectURL, "\r\n#") {
		return GitHubConfig{}, errors.New("GITHUB_OAUTH_REDIRECT_URL must be the frontend /auth/github/callback URL without credentials, query or fragment")
	}
	local := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return GitHubConfig{}, errors.New("GitHub OAuth redirect must use HTTPS; HTTP is allowed only on loopback")
	}
	if port := u.Port(); port != "" {
		number, parseErr := strconv.Atoi(port)
		if parseErr != nil || number < 1 || number > 65535 {
			return GitHubConfig{}, errors.New("GitHub OAuth redirect port is invalid")
		}
	}
	return result, nil
}
