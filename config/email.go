package config

import (
	"crypto/x509"
	"errors"
	"io"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// EmailConfig independently enables verification and recovery over a shared mail transport.
type EmailConfig struct {
	Enabled              bool
	PasswordResetEnabled bool
	PasswordResetTTL     time.Duration
	AppURL               string
	TokenTTL             time.Duration
	SMTP                 SMTPConfig
}

// SMTPConfig requires authenticated TLS delivery; credentials are never logged.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string
	RootCAs  *x509.CertPool
}

func loadEmailConfig() (EmailConfig, error) {
	result := EmailConfig{TokenTTL: 30 * time.Minute, PasswordResetTTL: 30 * time.Minute}
	value := getEnv("EMAIL_VERIFICATION_ENABLED", "false")
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return result, errors.New("EMAIL_VERIFICATION_ENABLED must be a boolean")
	}
	result.Enabled = enabled
	resetEnabled, err := strconv.ParseBool(getEnv("PASSWORD_RESET_ENABLED", "false"))
	if err != nil {
		return result, errors.New("PASSWORD_RESET_ENABLED must be a boolean")
	}
	result.PasswordResetEnabled = resetEnabled
	if resetEnabled {
		minutes, err := strconv.Atoi(getEnv("PASSWORD_RESET_TTL_MINUTES", "30"))
		if err != nil || minutes < 5 || minutes > 120 {
			return result, errors.New("PASSWORD_RESET_TTL_MINUTES must be between 5 and 120")
		}
		result.PasswordResetTTL = time.Duration(minutes) * time.Minute
	}
	if !enabled && !resetEnabled {
		return result, nil
	}
	result.AppURL = strings.TrimRight(os.Getenv("APP_PUBLIC_URL"), "/")
	u, err := url.Parse(result.AppURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
		return result, errors.New("APP_PUBLIC_URL must be an HTTPS origin (HTTP is allowed for localhost)")
	}
	if enabled {
		minutes, err := strconv.Atoi(getEnv("EMAIL_VERIFICATION_TTL_MINUTES", "30"))
		if err != nil || minutes < 5 || minutes > 120 {
			return result, errors.New("EMAIL_VERIFICATION_TTL_MINUTES must be between 5 and 120")
		}
		result.TokenTTL = time.Duration(minutes) * time.Minute
	}
	port, err := strconv.Atoi(getEnv("SMTP_PORT", "587"))
	if err != nil || port < 1 || port > 65535 {
		return result, errors.New("SMTP_PORT must be a valid port")
	}
	result.SMTP = SMTPConfig{Host: os.Getenv("SMTP_HOST"), Port: port, Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), From: os.Getenv("SMTP_FROM"), TLSMode: getEnv("SMTP_TLS_MODE", "starttls")}
	for _, value := range []string{result.SMTP.Host, result.SMTP.Username, result.SMTP.Password, result.SMTP.From} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
			return result, errors.New("SMTP_HOST, SMTP_USERNAME, SMTP_PASSWORD and SMTP_FROM are required without line breaks")
		}
	}
	address, err := mail.ParseAddress(result.SMTP.From)
	if err != nil || address.Address != result.SMTP.From {
		return result, errors.New("SMTP_FROM must be a plain mailbox address")
	}
	if result.SMTP.TLSMode != "starttls" && result.SMTP.TLSMode != "tls" {
		return result, errors.New("SMTP_TLS_MODE must be starttls or tls")
	}
	if path := os.Getenv("SMTP_CA_FILE"); path != "" {
		result.SMTP.RootCAs, err = loadSMTPRoots(path)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// loadSMTPRoots extends system trust for private SMTP providers, without bypassing TLS checks.
func loadSMTPRoots(path string) (*x509.CertPool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("SMTP_CA_FILE could not be opened")
	}
	defer file.Close()
	const maxPEMSize = 1 << 20
	pem, err := io.ReadAll(io.LimitReader(file, maxPEMSize+1))
	if err != nil || len(pem) > maxPEMSize {
		return nil, errors.New("SMTP_CA_FILE must be a readable PEM bundle of at most 1 MiB")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("system certificate trust could not be loaded")
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("SMTP_CA_FILE must contain valid PEM certificates")
	}
	return roots, nil
}
