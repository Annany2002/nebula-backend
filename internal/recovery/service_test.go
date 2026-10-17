package recovery

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
	"github.com/Annany2002/nebula-backend/internal/verification"
)

type testSender struct {
	mu       sync.Mutex
	body     string
	failure  bool
	notified chan struct{}
}

func (s *testSender) Send(_ context.Context, _, subject, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if subject == "Your Nebula password changed" {
		close(s.notified)
	}
	if s.failure {
		return errors.New("fixture delivery failure")
	}
	s.body = body
	return nil
}
func TestUnconfirmedRecoveryMailPreservesOlderLinkAndNotificationFailurePreservesReset(t *testing.T) {
	db, err := storage.ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	_, err = storage.CreateUser(ctx, db, "owner", "owner", "owner@example.test", "old-hash")
	require.NoError(t, err)
	sender := &testSender{notified: make(chan struct{})}
	settings := config.EmailConfig{AppURL: "https://app.example.test", PasswordResetTTL: 30 * time.Minute}
	service := New(db, &settings, sender)
	require.NoError(t, service.send(ctx, "owner", "owner@example.test"))
	sender.mu.Lock()
	body := sender.body
	sender.failure = true
	sender.mu.Unlock()
	start := strings.Index(body, "https://app.example.test/")
	require.NotEqual(t, -1, start)
	line := strings.SplitN(body[start:], "\n", 2)[0]
	u, err := url.Parse(line)
	require.NoError(t, err)
	fragment, err := url.ParseQuery(u.Fragment)
	require.NoError(t, err)
	token := fragment.Get("token")
	_, err = db.Exec(`UPDATE email_send_limits SET last_requested_at=last_requested_at-61`)
	require.NoError(t, err)
	require.Error(t, service.send(ctx, "owner", "owner@example.test"))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM account_password_reset_tokens`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, service.Reset(ctx, token, "NewPassword123!"))
	select {
	case <-sender.notified:
	case <-time.After(time.Second):
		t.Fatal("notification was not attempted")
	}
	user, err := storage.FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.True(t, auth.CheckPasswordHash("NewPassword123!", user.PasswordHash))
	require.Equal(t, 1, user.SessionVersion)
	// A change whose password check preceded recovery may not acquire fresh authorization afterward.
	mail := verification.New(db, &settings, sender)
	require.ErrorIs(t, mail.RequestChange(ctx, "owner", "attacker@example.test", 0), storage.ErrVerificationToken)
}

type blockingSender struct {
	entered chan struct{}
	release chan struct{}
}

func (s *blockingSender) Send(ctx context.Context, _, _, _ string) error {
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestRecoveryWorkerCapacityIsBoundedForEveryAddress(t *testing.T) {
	db, err := storage.ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	defer db.Close()
	sender := &blockingSender{entered: make(chan struct{}, 4), release: make(chan struct{})}
	service := New(db, &config.EmailConfig{AppURL: "https://app.example.test"}, sender)
	for _, id := range []string{"first", "second", "third", "fourth"} {
		_, err := storage.CreateUser(context.Background(), db, id, id, id+"@example.test", "hash")
		require.NoError(t, err)
		require.True(t, service.QueueRequest(id+"@example.test"))
	}
	for i := 0; i < 4; i++ {
		select {
		case <-sender.entered:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	require.False(t, service.QueueRequest("unknown@example.test"))
	require.False(t, service.QueueRequest("first@example.test"))
	close(sender.release)
	require.Eventually(t, func() bool { return len(service.slots) == 0 }, time.Second, 10*time.Millisecond)
}
