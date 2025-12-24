// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: Config
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"os"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/stretchr/testify/assert"
)

// ═══════════════════════════════════════════════════════════════════════════
// Test config.New()
// ═══════════════════════════════════════════════════════════════════════════

func TestConfigNew(t *testing.T) {
	cfg, err := config.New()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Server.Host)
	assert.NotEmpty(t, cfg.Server.Port)
}

func TestConfigNewWithEnvVars(t *testing.T) {
	os.Setenv("SERVER_HOST", "localhost")
	os.Setenv("SERVER_PORT", "3000")
	defer func() {
		os.Unsetenv("SERVER_HOST")
		os.Unsetenv("SERVER_PORT")
	}()

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, "3000", cfg.Server.Port)
}

func TestConfigServerWithEnvVars(t *testing.T) {
	os.Setenv("SERVER_HOST", "192.168.1.1")
	os.Setenv("SERVER_PORT", "9000")
	os.Setenv("SERVER_READ_TIMEOUT", "60s")
	defer func() {
		os.Unsetenv("SERVER_HOST")
		os.Unsetenv("SERVER_PORT")
		os.Unsetenv("SERVER_READ_TIMEOUT")
	}()

	cfg, _ := config.New()

	assert.Equal(t, "192.168.1.1", cfg.Server.Host)
	assert.Equal(t, "9000", cfg.Server.Port)
	assert.Equal(t, 60*time.Second, cfg.Server.ReadTimeout)
}

func TestConfigCORSWithEnvVars(t *testing.T) {
	os.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")
	os.Setenv("CORS_ALLOW_CREDENTIALS", "false")
	os.Setenv("CORS_MAX_AGE", "600")
	defer func() {
		os.Unsetenv("CORS_ALLOWED_ORIGINS")
		os.Unsetenv("CORS_ALLOW_CREDENTIALS")
		os.Unsetenv("CORS_MAX_AGE")
	}()

	cfg, _ := config.New()

	assert.Len(t, cfg.CORS.AllowedOrigins, 2)
	assert.False(t, cfg.CORS.AllowCredentials)
	assert.Equal(t, 600, cfg.CORS.MaxAge)
}

func TestConfigRateLimitWithEnvVars(t *testing.T) {
	os.Setenv("RATE_LIMIT_REQUESTS", "200")
	os.Setenv("RATE_LIMIT_WINDOW", "2m")
	defer func() {
		os.Unsetenv("RATE_LIMIT_REQUESTS")
		os.Unsetenv("RATE_LIMIT_WINDOW")
	}()

	cfg, _ := config.New()

	assert.Equal(t, 200, cfg.RateLimit.Requests)
	assert.Equal(t, 2*time.Minute, cfg.RateLimit.Window)
}

func TestConfigDefaults(t *testing.T) {
	os.Unsetenv("SERVER_HOST")
	os.Unsetenv("SERVER_PORT")

	cfg, _ := config.New()

	assert.NotEmpty(t, cfg.Server.Host)
	assert.NotEmpty(t, cfg.Server.Port)
	assert.NotEmpty(t, cfg.CORS.AllowedOrigins)
	assert.NotEmpty(t, cfg.CORS.AllowedMethods)
	assert.Greater(t, cfg.RateLimit.Requests, 0)
	assert.Greater(t, cfg.RateLimit.Window, time.Duration(0))
}
