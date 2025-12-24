// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: HTTP Server
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "localhost",
			Port:            "8080",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		CORS: contracts.CORSConfig{
			AllowedOrigins: []string{"*"},
		},
		RateLimit: contracts.RateLimitConfig{
			Requests: 100,
			Window:   time.Minute,
		},
	}
	logger := zerolog.Nop()

	server := appHttp.NewServer(cfg, logger)

	assert.NotNil(t, server)
	assert.NotNil(t, server.Router())
}

func TestServerRouter(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "localhost",
			Port:            "8080",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		CORS: contracts.CORSConfig{
			AllowedOrigins: []string{"*"},
		},
		RateLimit: contracts.RateLimitConfig{
			Requests: 100,
			Window:   time.Minute,
		},
	}
	logger := zerolog.Nop()

	server := appHttp.NewServer(cfg, logger)
	router := server.Router()

	assert.NotNil(t, router)
}
