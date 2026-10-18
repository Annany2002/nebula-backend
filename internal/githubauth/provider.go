// Package githubauth implements the browser-bound GitHub authorization-code flow.
package githubauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/verification"
)

var (
	// ErrProvider never includes upstream response bodies, codes or credentials.
	ErrProvider = errors.New("GitHub authentication is unavailable")
	// ErrProviderCode indicates that the provider rejected the one-use authorization code.
	ErrProviderCode = errors.New("GitHub authorization code was rejected")
	// ErrVerifiedEmail requires authoritative primary-email proof, never the public profile field.
	ErrVerifiedEmail = errors.New("GitHub has no verified primary email")
	loginPattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

// Profile contains only the identity needed to resolve a Nebula account.
type Profile struct{ Subject, Login, Email string }

// Provider keeps remote credential exchange injectable without exposing configurable endpoints.
type Provider interface {
	Exchange(context.Context, string, string) (Profile, error)
}

type githubProvider struct {
	settings config.GitHubConfig
	client   *http.Client
}

// NewProvider uses fixed HTTPS GitHub endpoints, no redirects, no retries and bounded responses.
func NewProvider(settings config.GitHubConfig) Provider {
	return &githubProvider{settings: settings, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (p *githubProvider) Exchange(ctx context.Context, code, verifier string) (Profile, error) {
	var profile Profile
	values := url.Values{"client_id": {p.settings.ClientID}, "client_secret": {p.settings.ClientSecret}, "code": {code}, "redirect_uri": {p.settings.RedirectURL}, "code_verifier": {verifier}}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err := p.request(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", values, "", &token); err != nil {
		return profile, err
	}
	if token.Error != "" {
		if token.Error == "bad_verification_code" || token.Error == "incorrect_code_verifier" {
			return profile, ErrProviderCode
		}
		return profile, ErrProvider
	}
	if token.AccessToken == "" || len(token.AccessToken) > 2048 || !strings.EqualFold(token.TokenType, "bearer") || strings.IndexFunc(token.AccessToken, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return profile, ErrProvider
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := p.request(ctx, http.MethodGet, "https://api.github.com/user", nil, token.AccessToken, &user); err != nil {
		return profile, err
	}
	if user.ID <= 0 || user.Type != "User" || !loginPattern.MatchString(user.Login) {
		return profile, ErrProvider
	}
	// An account can have more than one page of addresses. Bound traversal and reject ambiguity.
	address := ""
	for page := 1; page <= 10; page++ {
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		endpoint := "https://api.github.com/user/emails?per_page=100&page=" + strconv.Itoa(page)
		if err := p.request(ctx, http.MethodGet, endpoint, nil, token.AccessToken, &emails); err != nil {
			return profile, err
		}
		for _, item := range emails {
			if item.Primary && item.Verified {
				if address != "" || !verification.ValidateEmail(item.Email) {
					return profile, ErrVerifiedEmail
				}
				address = item.Email
			}
		}
		if len(emails) < 100 {
			if address == "" {
				return profile, ErrVerifiedEmail
			}
			return Profile{Subject: strconv.FormatInt(user.ID, 10), Login: user.Login, Email: address}, nil
		}
	}
	return profile, ErrVerifiedEmail
}

func (p *githubProvider) request(ctx context.Context, method, endpoint string, values url.Values, token string, target any) error {
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return ErrProvider
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Nebula-GitHub-Sign-In")
	req.Header.Set("Cache-Control", "no-store")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	}
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrProvider
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrProvider
	}
	const limit = 128 * 1024
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrProvider
	}
	if len(data) > limit || json.Unmarshal(data, target) != nil {
		return ErrProvider
	}
	return nil
}
