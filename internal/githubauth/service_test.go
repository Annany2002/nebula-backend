package githubauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

type blockingProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingProvider) Exchange(ctx context.Context, _, _ string) (Profile, error) {
	p.entered <- struct{}{}
	select {
	case <-p.release:
		return Profile{}, ErrProvider
	case <-ctx.Done():
		return Profile{}, ctx.Err()
	}
}

func TestCallbackCapacityIsBoundedAndDoesNotConsumeRejectedState(t *testing.T) {
	db, err := storage.ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	defer db.Close()
	p := &blockingProvider{entered: make(chan struct{}, 8), release: make(chan struct{})}
	defer close(p.release)
	service := New(db, config.GitHubConfig{Enabled: true, ClientID: "fixture", RedirectURL: "https://app.example.test/auth/github/callback"}, p, false)
	verifier := strings.Repeat("x", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	states := make([]string, 9)
	for i := range states {
		start, err := service.Start(context.Background(), challenge, "peer", storage.GitHubLinkIntent{})
		require.NoError(t, err)
		u, err := url.Parse(start.AuthorizationURL)
		require.NoError(t, err)
		states[i] = u.Query().Get("state")
	}
	results := make(chan error, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 8; i++ {
		go func(index int) { _, _, err := service.Complete(ctx, "code", states[index], verifier); results <- err }(i)
	}
	for i := 0; i < 8; i++ {
		<-p.entered
	}
	_, _, err = service.Complete(ctx, "code", states[8], verifier)
	require.ErrorIs(t, err, ErrBusy)
	cancel()
	for i := 0; i < 8; i++ {
		require.ErrorIs(t, <-results, context.Canceled)
	}
	var unclaimed int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM github_oauth_states WHERE claimed=0`).Scan(&unclaimed))
	require.Equal(t, 1, unclaimed)
	require.Zero(t, len(service.slots))
}
