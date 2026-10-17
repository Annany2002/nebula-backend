// Package email provides bounded, authenticated SMTP delivery without logging message contents.
package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/Annany2002/nebula-backend/config"
)

// Sender is the shared boundary for verification and account recovery emails.
type Sender interface {
	Send(context.Context, string, string, string) error
}

// SMTP sends plain-text account mail over verified TLS.
type SMTP struct {
	settings  config.SMTPConfig
	tlsConfig *tls.Config
}

// NewSMTP constructs the sender from validated application configuration.
func NewSMTP(settings *config.SMTPConfig) *SMTP {
	tlsConfig := &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}
	if settings.RootCAs != nil {
		tlsConfig.RootCAs = settings.RootCAs.Clone()
	}
	return &SMTP{settings: *settings, tlsConfig: tlsConfig}
}

// Send bounds the entire SMTP conversation, including stalled server responses.
func (s *SMTP) Send(ctx context.Context, recipient, subject, body string) error {
	if s.settings.TLSMode != "tls" && s.settings.TLSMode != "starttls" {
		return errors.New("email delivery requires TLS")
	}
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient || strings.ContainsAny(recipient+subject+s.settings.From, "\r\n") {
		return errors.New("invalid email headers")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.settings.Host, strconv.Itoa(s.settings.Port)))
	if err != nil {
		return errors.New("email delivery connection failed")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return errors.New("email delivery deadline failed")
	}
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	tlsConfig := s.tlsConfig.Clone()
	if s.settings.TLSMode == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err = secure.HandshakeContext(ctx); err != nil {
			return errors.New("email delivery TLS failed")
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, s.settings.Host)
	if err != nil {
		return errors.New("email delivery handshake failed")
	}
	defer client.Close()
	if s.settings.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("email delivery requires STARTTLS")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return errors.New("email delivery TLS failed")
		}
	}
	if err = client.Auth(smtp.PlainAuth("", s.settings.Username, s.settings.Password, s.settings.Host)); err != nil {
		return errors.New("email delivery authentication failed")
	}
	if err = client.Mail(s.settings.From); err != nil {
		return errors.New("email delivery sender rejected")
	}
	if err = client.Rcpt(recipient); err != nil {
		return errors.New("email delivery recipient rejected")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("email delivery data rejected")
	}
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", s.settings.From, recipient, subject, strings.ReplaceAll(body, "\n", "\r\n"))
	if _, err = writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return errors.New("email delivery write failed")
	}
	if err = writer.Close(); err != nil {
		return errors.New("email delivery was not confirmed")
	}
	// DATA acceptance is the delivery acknowledgement; a failed QUIT cannot unsend mail.
	_ = client.Quit()
	return nil
}
