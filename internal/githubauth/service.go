package githubauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"time"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/accounttoken"
	"github.com/Annany2002/nebula-backend/internal/domain"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

var (
	// ErrDisabled keeps unconfigured deployments usable without OAuth credentials.
	ErrDisabled = errors.New("GitHub sign-in is not configured")
	// ErrBusy bounds concurrent provider exchanges without spawning background work.
	ErrBusy = errors.New("GitHub sign-in capacity is busy")
	// ErrRequest rejects malformed PKCE or callback material before provider work.
	ErrRequest      = errors.New("invalid GitHub sign-in request")
	verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	codePattern     = regexp.MustCompile(`^[\x21-\x7E]{1,512}$`)
)

// Service owns bounded transaction state and never persists GitHub access tokens.
type Service struct {
	slots                chan struct{}
	db                   *sql.DB
	settings             config.GitHubConfig
	provider             Provider
	verificationRequired bool
}

// New constructs the OAuth boundary with a replaceable provider for hermetic tests.
func New(db *sql.DB, settings config.GitHubConfig, provider Provider, verificationRequired bool) *Service {
	return &Service{slots: make(chan struct{}, 8), db: db, settings: settings, provider: provider, verificationRequired: verificationRequired}
}

// StartResult gives the browser the fixed-provider URL and transaction lifetime.
type StartResult struct {
	AuthorizationURL string `json:"authorization_url"`
	ExpiresIn        int    `json:"expires_in"`
}

// Start binds a generated state to the client's S256 challenge and optional reauthenticated owner.
func (s *Service) Start(ctx context.Context, challenge, peer string, link storage.GitHubLinkIntent) (StartResult, error) {
	var result StartResult
	if !s.settings.Enabled {
		return result, ErrDisabled
	}
	if !accounttoken.Valid(challenge) {
		return result, ErrRequest
	}
	state, err := accounttoken.New()
	if err != nil {
		return result, err
	}
	if err := storage.ReserveGitHubState(ctx, s.db, accounttoken.Digest(state), challenge, accounttoken.Digest(peer), link, time.Now()); err != nil {
		return result, err
	}
	values := url.Values{"client_id": {s.settings.ClientID}, "redirect_uri": {s.settings.RedirectURL}, "scope": {"user:email"}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "prompt": {"select_account"}}
	return StartResult{AuthorizationURL: "https://github.com/login/oauth/authorize?" + values.Encode(), ExpiresIn: 600}, nil
}

// Complete claims state before the non-retryable exchange. Failures require a fresh start.
func (s *Service) Complete(ctx context.Context, code, state, verifier string) (*domain.UserMetadata, bool, error) {
	if !s.settings.Enabled {
		return nil, false, ErrDisabled
	}
	if !accounttoken.Valid(state) || !verifierPattern.MatchString(verifier) || !codePattern.MatchString(code) {
		return nil, false, ErrRequest
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return nil, false, ErrBusy
	}
	defer func() { <-s.slots }()
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	intent, err := storage.ClaimGitHubState(ctx, s.db, accounttoken.Digest(state), challenge, time.Now())
	if err != nil {
		return nil, false, err
	}
	if intent.UserID != "" {
		access, readErr := storage.GetEmailVerificationStatus(ctx, s.db, intent.UserID)
		if readErr != nil {
			return nil, false, readErr
		}
		if access.Suspended || (access.Required && !access.Verified) || access.SessionVersion != intent.SessionVersion {
			return nil, false, storage.ErrGitHubAccount
		}
	}
	profile, err := s.provider.Exchange(ctx, code, verifier)
	if err != nil {
		return nil, false, err
	}
	user, err := storage.ResolveGitHubAccount(ctx, s.db, profile.Subject, profile.Login, profile.Email, s.verificationRequired, intent)
	return user, intent.UserID != "", err
}
