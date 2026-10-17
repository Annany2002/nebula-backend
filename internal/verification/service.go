// Package verification owns email-token generation and delivery independently of HTTP.
package verification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/email"
	"github.com/Annany2002/nebula-backend/internal/logger"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// Service persists intent before sending mail; usable tokens never leave the sender boundary.
type Service struct {
	db       *sql.DB
	sender   email.Sender
	settings config.EmailConfig
	slots    chan struct{}
}

// New constructs the account verification service with an injectable mail sender.
func New(db *sql.DB, settings *config.EmailConfig, sender email.Sender) *Service {
	return &Service{db: db, settings: *settings, sender: sender, slots: make(chan struct{}, 4)}
}

// QueueResend gives all addresses the same response path and bounds detached SMTP work.
func (s *Service) QueueResend(address string) bool {
	select {
	case s.slots <- struct{}{}:
	default:
		return false
	}
	go func() {
		defer func() { <-s.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		user, err := storage.FindUserByEmail(ctx, s.db, address)
		if errors.Is(err, storage.ErrUserNotFound) {
			return
		}
		if err == nil && !user.EmailVerified {
			err = s.Send(ctx, user.UserId, user.Email, false)
		}
		if err != nil && !errors.Is(err, storage.ErrVerificationRateLimit) {
			logger.NewLogger().Warn("Verification delivery was not confirmed")
		}
	}()
	return true
}

// ValidateEmail preserves the local part and rejects display names, headers and SMTPUTF8.
func ValidateEmail(value string) bool {
	if len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	for _, char := range value {
		if char > 127 {
			return false
		}
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

// Send issues an address-bound token. Failed mail keeps the account unverified and retryable.
func (s *Service) Send(ctx context.Context, id, target string, change bool) error {
	user, err := storage.FindUserByUserId(ctx, s.db, id)
	if err != nil {
		return err
	}
	if !ValidateEmail(target) {
		return errors.New("invalid email address")
	}
	bytes := make([]byte, 32)
	if _, err = rand.Read(bytes); err != nil {
		return errors.New("verification token generation failed")
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	digest := hashToken(token)
	purpose := "verify_email"
	if change {
		purpose = "change_email"
	}
	now := time.Now().UTC()
	ttl := s.settings.TokenTTL
	if ttl == 0 {
		ttl = 30 * time.Minute
	}
	if err := storage.ReserveEmailToken(ctx, s.db, id, user.Email, target, purpose, digest, now, now.Add(ttl)); err != nil {
		return err
	}
	if change {
		err = s.sender.Send(ctx, user.Email, "Nebula email change requested", "A request was made to change your Nebula email address. Your current address remains active until the new address is confirmed. If you did not request this, secure your account.")
	}
	if err == nil {
		link := s.settings.AppURL + "/verify-email#" + url.Values{"token": {token}}.Encode()
		err = s.sender.Send(ctx, target, "Verify your Nebula email", "Confirm your email address by opening this link:\n\n"+link+"\n\nThis link expires in "+ttl.String()+" and can be used once. If you did not request this, ignore this message.")
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if discardErr := storage.DiscardEmailToken(cleanup, s.db, digest); discardErr != nil {
			return errors.New("email delivery and token cleanup were not confirmed")
		}
		return errors.New("email delivery was not confirmed")
	}
	return nil
}

// Verify atomically consumes a canonical single-use token; it never returns a login session.
func (s *Service) Verify(ctx context.Context, token string) error {
	bytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(bytes) != 32 || base64.RawURLEncoding.EncodeToString(bytes) != token {
		return storage.ErrVerificationToken
	}
	return storage.ConsumeEmailToken(ctx, s.db, hashToken(token), time.Now().UTC())
}

func hashToken(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
