package config

import (
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEmailConfigurationFailsClosed(t *testing.T) {
	t.Setenv("SMTP_CA_FILE", "")
	for _, tc := range []struct{ name, key, value string }{
		{"missing host", "SMTP_HOST", ""}, {"insecure origin", "APP_PUBLIC_URL", "http://example.com"},
		{"origin credentials", "APP_PUBLIC_URL", "https://user:pass@example.com"}, {"origin query", "APP_PUBLIC_URL", "https://example.com?token=x"},
		{"origin path", "APP_PUBLIC_URL", "https://example.com/app"}, {"bad ttl", "EMAIL_VERIFICATION_TTL_MINUTES", "121"},
		{"bad port", "SMTP_PORT", "0"}, {"plaintext mode", "SMTP_TLS_MODE", "none"}, {"header injection", "SMTP_FROM", "a@example.com\r\nBcc: other@example.com"},
		{"invalid boolean", "EMAIL_VERIFICATION_ENABLED", "sometimes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range map[string]string{"EMAIL_VERIFICATION_ENABLED": "true", "APP_PUBLIC_URL": "https://app.example.com", "SMTP_HOST": "smtp.example.com", "SMTP_PORT": "587", "SMTP_USERNAME": "test-user", "SMTP_PASSWORD": "test-password", "SMTP_FROM": "nebula@example.com", "SMTP_TLS_MODE": "starttls", "EMAIL_VERIFICATION_TTL_MINUTES": "30"} {
				t.Setenv(key, value)
			}
			t.Setenv(tc.key, tc.value)
			_, err := loadEmailConfig()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "test-password")
		})
	}
}

func TestEmailConfigurationDisabledAndLocalDevelopment(t *testing.T) {
	t.Setenv("SMTP_CA_FILE", "")
	t.Setenv("EMAIL_VERIFICATION_ENABLED", "false")
	t.Setenv("SMTP_PASSWORD", "")
	cfg, err := loadEmailConfig()
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	for key, value := range map[string]string{"EMAIL_VERIFICATION_ENABLED": "true", "APP_PUBLIC_URL": "http://localhost:3000", "SMTP_HOST": "smtp.example.com", "SMTP_USERNAME": "test-user", "SMTP_PASSWORD": "test-password", "SMTP_FROM": "nebula@example.com", "SMTP_TLS_MODE": "tls", "SMTP_PORT": "465", "EMAIL_VERIFICATION_TTL_MINUTES": "30"} {
		t.Setenv(key, value)
	}
	cfg, err = loadEmailConfig()
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, cfg.TokenTTL)
	require.Equal(t, 465, cfg.SMTP.Port)
}

func TestSMTPPrivateCertificateTrust(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "smtp-ca.pem")
	certificate := server.Certificate()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0o600))
	for key, value := range map[string]string{
		"EMAIL_VERIFICATION_ENABLED": "true", "APP_PUBLIC_URL": "http://localhost:3000",
		"SMTP_HOST": "localhost", "SMTP_USERNAME": "local", "SMTP_PASSWORD": "local-password",
		"SMTP_FROM": "nebula@example.test", "SMTP_TLS_MODE": "starttls", "SMTP_PORT": "1025",
		"EMAIL_VERIFICATION_TTL_MINUTES": "30", "SMTP_CA_FILE": path,
	} {
		t.Setenv(key, value)
	}
	cfg, err := loadEmailConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.SMTP.RootCAs)
	_, err = certificate.Verify(x509.VerifyOptions{Roots: cfg.SMTP.RootCAs, DNSName: "127.0.0.1"})
	require.NoError(t, err)
	_, err = certificate.Verify(x509.VerifyOptions{Roots: cfg.SMTP.RootCAs, DNSName: "different.example.test"})
	require.Error(t, err, "a custom CA must not disable hostname verification")
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"invalid PEM", []byte("not a certificate")},
		{"oversized PEM", make([]byte, (1<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, tc.data, 0o600))
			_, err := loadEmailConfig()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "local-password")
		})
	}
	t.Setenv("SMTP_CA_FILE", path+".missing")
	_, err = loadEmailConfig()
	require.ErrorContains(t, err, "could not be opened")
	t.Setenv("EMAIL_VERIFICATION_ENABLED", "false")
	_, err = loadEmailConfig()
	require.NoError(t, err, "disabled email must not require a CA file")
}

func TestPasswordRecoveryCanRunIndependentlyOfSignupVerification(t *testing.T) {
	for key, value := range map[string]string{
		"EMAIL_VERIFICATION_ENABLED": "false", "PASSWORD_RESET_ENABLED": "true", "PASSWORD_RESET_TTL_MINUTES": "45",
		"APP_PUBLIC_URL": "http://localhost:3000", "SMTP_HOST": "smtp.example.test", "SMTP_USERNAME": "fixture", "SMTP_PASSWORD": "fixture-password", "SMTP_FROM": "nebula@example.test", "SMTP_TLS_MODE": "starttls", "SMTP_PORT": "587", "SMTP_CA_FILE": "",
	} {
		t.Setenv(key, value)
	}
	cfg, err := loadEmailConfig()
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.True(t, cfg.PasswordResetEnabled)
	require.Equal(t, 45*time.Minute, cfg.PasswordResetTTL)
	t.Setenv("EMAIL_VERIFICATION_TTL_MINUTES", "invalid-but-disabled")
	_, err = loadEmailConfig()
	require.NoError(t, err)
	for _, value := range []string{"4", "121", "invalid"} {
		t.Setenv("PASSWORD_RESET_TTL_MINUTES", value)
		_, err = loadEmailConfig()
		require.Error(t, err)
	}
	t.Setenv("PASSWORD_RESET_TTL_MINUTES", "30")
	t.Setenv("SMTP_PASSWORD", "")
	_, err = loadEmailConfig()
	require.Error(t, err)
	t.Setenv("PASSWORD_RESET_ENABLED", "false")
	_, err = loadEmailConfig()
	require.NoError(t, err)
	t.Setenv("PASSWORD_RESET_ENABLED", "invalid")
	_, err = loadEmailConfig()
	require.Error(t, err)
}
