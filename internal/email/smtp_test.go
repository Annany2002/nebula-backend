package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
)

func TestSMTPAuthenticatedTLSDelivery(t *testing.T) {
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			fixture := httptest.NewTLSServer(nil)
			certificate := fixture.TLS.Certificates[0]
			root := fixture.Certificate()
			fixture.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			messages := make(chan string, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if mode == "tls" {
					conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
				}
				reader := bufio.NewReader(conn)
				_, _ = fmt.Fprint(conn, "220 fixture SMTP\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = fmt.Fprint(conn, "250-fixture\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(line, "STARTTLS"):
						_, _ = fmt.Fprint(conn, "220 begin TLS\r\n")
						conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
						reader = bufio.NewReader(conn)
					case strings.HasPrefix(line, "AUTH"):
						_, _ = fmt.Fprint(conn, "235 authenticated\r\n")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						_, _ = fmt.Fprint(conn, "250 accepted\r\n")
					case strings.HasPrefix(line, "DATA"):
						_, _ = fmt.Fprint(conn, "354 send message\r\n")
						var body strings.Builder
						for {
							part, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							if part == ".\r\n" {
								break
							}
							body.WriteString(part)
						}
						messages <- body.String()
						_, _ = fmt.Fprint(conn, "250 queued\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = fmt.Fprint(conn, "221 bye\r\n")
						return
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			number, _ := strconv.Atoi(port)
			pool := x509.NewCertPool()
			pool.AddCert(root)
			sender := NewSMTP(&config.SMTPConfig{Host: host, Port: number, Username: "fixture-user", Password: "fixture-password", From: "nebula@example.test", TLSMode: mode, RootCAs: pool})
			require.NoError(t, sender.Send(context.Background(), "owner@example.test", "Verify email", "Verify using https://app.example.test/verify-email#token=fixture"))
			message := <-messages
			require.Contains(t, message, "To: owner@example.test\r\n")
			require.Contains(t, message, "Subject: Verify email\r\n")
			require.NotContains(t, message, "fixture-password")
			<-finished
		})
	}
}

func TestSMTPRejectsInjectionPlaintextAndCancelledRequests(t *testing.T) {
	sender := NewSMTP(&config.SMTPConfig{Host: "localhost", Port: 1, From: "nebula@example.test", TLSMode: "none"})
	require.ErrorContains(t, sender.Send(context.Background(), "owner@example.test", "Verify", "body"), "requires TLS")
	sender.settings.TLSMode = "starttls"
	require.ErrorContains(t, sender.Send(context.Background(), "owner@example.test\r\nBcc: victim@example.test", "Verify", "body"), "headers")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, sender.Send(ctx, "owner@example.test", "Verify", "body"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = fmt.Fprint(conn, "220 fixture SMTP\r\n")
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = fmt.Fprint(conn, "250 plaintext only\r\n")
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	sender = NewSMTP(&config.SMTPConfig{Host: host, Port: number, From: "nebula@example.test", TLSMode: "starttls"})
	require.ErrorContains(t, sender.Send(context.Background(), "owner@example.test", "Verify", "body"), "STARTTLS")
	<-finished
}

func TestSMTPRejectsUntrustedCertificate(t *testing.T) {
	fixture := httptest.NewTLSServer(nil)
	defer fixture.Close()
	host, port, err := net.SplitHostPort(fixture.Listener.Addr().String())
	require.NoError(t, err)
	number, err := strconv.Atoi(port)
	require.NoError(t, err)
	sender := NewSMTP(&config.SMTPConfig{Host: host, Port: number, From: "nebula@example.test", TLSMode: "tls"})
	require.ErrorContains(t, sender.Send(context.Background(), "owner@example.test", "Verify", "body"), "TLS failed")
}

func TestSMTPCancellationInterruptsStalledGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	accepted := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		close(accepted)
		// Wait for the client's cancellation to close the connection, without a greeting.
		buffer := make([]byte, 1)
		_, _ = conn.Read(buffer)
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	number, err := strconv.Atoi(port)
	require.NoError(t, err)
	sender := NewSMTP(&config.SMTPConfig{Host: host, Port: number, From: "nebula@example.test", TLSMode: "starttls"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- sender.Send(ctx, "owner@example.test", "Verify", "body") }()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("SMTP fixture did not accept the connection")
	}
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("SMTP did not stop after cancellation")
	}
	<-finished
}
