// Package recovery owns password-reset intent, delivery and credential replacement.
package recovery

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"time"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/accounttoken"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/email"
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// Service bounds detached mail work; raw reset tokens only cross the mail-sender boundary.
type Service struct {
	db       *sql.DB
	settings config.EmailConfig
	sender   email.Sender
	slots    chan struct{}
}

// New constructs recovery with an injectable authenticated mail sender.
func New(db *sql.DB, settings *config.EmailConfig, sender email.Sender) *Service {
	return &Service{db: db, settings: *settings, sender: sender, slots: make(chan struct{}, 4)}
}

// QueueRequest returns on the same path for every syntactically valid address.
func (s *Service) QueueRequest(address string) bool {
	return s.queue(func(ctx context.Context) {
		user, err := storage.FindUserByEmail(ctx, s.db, address)
		if errors.Is(err, storage.ErrUserNotFound) {
			return
		}
		if err == nil {
			err = s.send(ctx, user.UserId, user.Email)
		}
		if err != nil && !errors.Is(err, storage.ErrVerificationRateLimit) && !errors.Is(err, storage.ErrPasswordResetToken) {
			logger.NewLogger().Warn("Password reset delivery was not confirmed")
		}
	})
}

func (s *Service) queue(work func(context.Context)) bool {
	select {
	case s.slots <- struct{}{}:
	default:
		return false
	}
	go func() {
		defer func() { <-s.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		work(ctx)
	}()
	return true
}

func (s *Service) send(ctx context.Context, id, address string) error {
	token, err := accounttoken.New()
	if err != nil {
		return errors.New("password reset token generation failed")
	}
	digest := accounttoken.Digest(token)
	now := time.Now().UTC()
	ttl := s.settings.PasswordResetTTL
	if ttl == 0 {
		ttl = 30 * time.Minute
	}
	if err := storage.ReservePasswordResetToken(ctx, s.db, id, address, digest, now, now.Add(ttl)); err != nil {
		return err
	}
	link := s.settings.AppURL + "/reset-password#" + url.Values{"token": {token}}.Encode()
	err = s.sender.Send(ctx, address, "Reset your Nebula password", "Open this link to choose a new password:\n\n"+link+"\n\nThis link expires in "+ttl.String()+" and can be used once. If you did not request this, ignore this message. Your password has not changed.")
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if cleanupErr := storage.DiscardPasswordResetToken(cleanup, s.db, digest); cleanupErr != nil {
			return errors.New("password reset delivery and cleanup were not confirmed")
		}
		return errors.New("password reset delivery was not confirmed")
	}
	return nil
}

// Reset requires explicit submission; mail scanners and GET requests cannot consume a link.
func (s *Service) Reset(ctx context.Context, token, password string) error {
	if !accounttoken.Valid(token) {
		return storage.ErrPasswordResetToken
	}
	digest := accounttoken.Digest(token)
	eligible, err := storage.PasswordResetEligible(ctx, s.db, digest, time.Now().UTC())
	if err != nil {
		return err
	}
	if !eligible {
		return storage.ErrPasswordResetToken
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	address, err := storage.ConsumePasswordReset(ctx, s.db, digest, passwordHash, time.Now().UTC())
	if err != nil {
		return err
	}
	// Notification failure cannot undo a committed credential change or invite a write retry.
	if !s.queue(func(ctx context.Context) {
		if err := s.sender.Send(ctx, address, "Your Nebula password changed", "Your Nebula password was reset. Existing sign-in sessions were revoked. Sign in with your new password. If you did not make this change, contact the platform operator immediately."); err != nil {
			logger.NewLogger().Warn("Password change notification was not confirmed")
		}
	}) {
		logger.NewLogger().Warn("Password change notification was not queued")
	}
	return nil
}
