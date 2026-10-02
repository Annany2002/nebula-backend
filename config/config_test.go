package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigUsesAValidListeningPort(t *testing.T) {
	for _, tc := range []struct {
		name, environment, port, want string
		unsetPort, invalid            bool
	}{
		{name: "development default", environment: "development", unsetPort: true, want: "8080"},
		{name: "production default", environment: "production", unsetPort: true, want: "8085"},
		{name: "explicit production port", environment: "production", port: "8085", want: "8085"},
		{name: "legacy leading colon", environment: "production", port: ":8085", want: "8085"},
		{name: "invalid port", environment: "production", port: "not-a-port", invalid: true},
		{name: "out of range", environment: "production", port: "65536", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.environment)
			t.Setenv("JWT_SECRET", "test-secret")
			t.Setenv("SERVER_PORT", tc.port)
			if tc.unsetPort {
				require.NoError(t, os.Unsetenv("SERVER_PORT"))
			}
			cfg, err := LoadConfig()
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.ServerPort)
		})
	}
}

func TestLoadConfigReturnsErrorWhenJWTSecretIsMissing(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "")
	require.NoError(t, os.Unsetenv("JWT_SECRET"))
	_, err := LoadConfig()
	require.ErrorContains(t, err, "JWT_SECRET")
}
