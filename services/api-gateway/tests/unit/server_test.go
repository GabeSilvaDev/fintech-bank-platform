// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: HTTP Server
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"context"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func testServerConfig() *config.Config {
	return &config.Config{
		Server: contracts.ServerConfig{
			Host:            "127.0.0.1",
			Port:            "0",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		CORS: contracts.CORSConfig{
			AllowedOrigins: []string{"*"},
		},
		RateLimit: contracts.RateLimitConfig{
			Requests: 100,
			Window:   time.Minute,
		},
	}
}

func TestNewServer(t *testing.T) {
	cfg := testServerConfig()
	logger := zerolog.Nop()

	server := appHttp.NewServer(cfg, logger)

	assert.NotNil(t, server)
	assert.NotNil(t, server.Router())
}

func TestServerRouter(t *testing.T) {
	cfg := testServerConfig()
	logger := zerolog.Nop()

	server := appHttp.NewServer(cfg, logger)
	router := server.Router()

	assert.NotNil(t, router)
}

func TestServerAddress(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "localhost",
			Port:            "9090",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
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
	
	assert.Equal(t, "localhost:9090", server.Address())
}

func TestServerShutdown(t *testing.T) {
	cfg := testServerConfig()
	logger := zerolog.Nop()

	server := appHttp.NewServer(cfg, logger)
	appHttp.SetupRouter(server.Router(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestServerStartWithSignals(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "127.0.0.1",
			Port:            "18080",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
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
	appHttp.SetupRouter(server.Router(), cfg)

	done := make(chan error, 1)

	go func() {
		done <- server.StartWithSignals(syscall.SIGUSR1)
	}()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:18080/health")
	if err == nil {
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not shut down in time")
	}
}

func TestServerStartWithInvalidPort(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "127.0.0.1",
			Port:            "invalid",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
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

	done := make(chan error, 1)

	go func() {
		done <- server.StartWithSignals(syscall.SIGUSR1)
	}()

	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Expected error for invalid port")
	}
}

func TestServerStart(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "127.0.0.1",
			Port:            "18081",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 5 * time.Second,
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
	appHttp.SetupRouter(server.Router(), cfg)

	done := make(chan error, 1)

	go func() {
		done <- server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:18081/health")
	if err == nil {
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not shut down in time")
	}
}

func TestServerStartWithShutdownTimeout(t *testing.T) {
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host:            "127.0.0.1",
			Port:            "18082",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 1 * time.Nanosecond,
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
	appHttp.SetupRouter(server.Router(), cfg)

	server.Router().Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	done := make(chan error, 1)

	go func() {
		done <- server.StartWithSignals(syscall.SIGUSR2)
	}()

	time.Sleep(100 * time.Millisecond)

	go func() {
		http.Get("http://127.0.0.1:18082/slow")
	}()

	time.Sleep(50 * time.Millisecond)

	syscall.Kill(syscall.Getpid(), syscall.SIGUSR2)

	select {
	case err := <-done:
		if err != nil {
			assert.Error(t, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not shut down in time")
	}
}
