package unit

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigRefusesAMissingJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	os.Unsetenv("JWT_SECRET")

	cfg, err := config.New()

	assert.ErrorIs(t, err, config.ErrWeakJWTSecret)
	assert.Nil(t, cfg)
}

func TestConfigRefusesAShortJWTSecret(t *testing.T) {
	for _, secret := range []string{"", "short", strings.Repeat("s", 31)} {
		t.Setenv("JWT_SECRET", secret)

		cfg, err := config.New()

		assert.ErrorIs(t, err, config.ErrWeakJWTSecret, secret)
		assert.Nil(t, cfg)
	}
}

func TestConfigAcceptsAJWTSecretOfThirtyTwoBytes(t *testing.T) {
	secret := strings.Repeat("s", 32)
	t.Setenv("JWT_SECRET", secret)

	cfg, err := config.New()

	require.NoError(t, err)
	assert.Equal(t, secret, cfg.Auth.JWTSecret)
}

func TestConfigAuthDefaults(t *testing.T) {
	cfg, err := config.New()

	require.NoError(t, err)
	assert.Equal(t, tests.JWTSecret, cfg.Auth.JWTSecret)
	assert.Equal(t, time.Hour, cfg.Auth.TokenTTL)
	assert.Equal(t, time.Minute, cfg.Auth.OwnerCacheTTL)
	assert.Equal(t, 10, cfg.AuthRateLimit.Requests)
	assert.Equal(t, time.Minute, cfg.AuthRateLimit.Window)
}

func TestConfigAuthFromEnv(t *testing.T) {
	t.Setenv("JWT_TTL", "15m")
	t.Setenv("OWNER_CACHE_TTL", "30s")
	t.Setenv("AUTH_RATE_LIMIT_REQUESTS", "3")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW", "5m")

	cfg, err := config.New()

	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, cfg.Auth.TokenTTL)
	assert.Equal(t, 30*time.Second, cfg.Auth.OwnerCacheTTL)
	assert.Equal(t, 3, cfg.AuthRateLimit.Requests)
	assert.Equal(t, 5*time.Minute, cfg.AuthRateLimit.Window)
}

func TestConfigAuthNonPositiveValuesFallBack(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Setenv("JWT_TTL", value+"s")
		t.Setenv("OWNER_CACHE_TTL", value+"m")
		t.Setenv("AUTH_RATE_LIMIT_REQUESTS", value)
		t.Setenv("AUTH_RATE_LIMIT_WINDOW", value+"s")

		cfg, err := config.New()

		require.NoError(t, err)
		assert.Equal(t, time.Hour, cfg.Auth.TokenTTL, value)
		assert.Equal(t, time.Minute, cfg.Auth.OwnerCacheTTL, value)
		assert.Equal(t, 10, cfg.AuthRateLimit.Requests, value)
		assert.Equal(t, time.Minute, cfg.AuthRateLimit.Window, value)
	}
}

func TestConfigAuthInvalidValuesFallBack(t *testing.T) {
	t.Setenv("JWT_TTL", "soon")
	t.Setenv("AUTH_RATE_LIMIT_REQUESTS", "many")

	cfg, err := config.New()

	require.NoError(t, err)
	assert.Equal(t, time.Hour, cfg.Auth.TokenTTL)
	assert.Equal(t, 10, cfg.AuthRateLimit.Requests)
}
