package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/joho/godotenv"
)

type Config struct {
	Server    contracts.ServerConfig
	CORS      contracts.CORSConfig
	RateLimit contracts.RateLimitConfig
}

func New() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		Server:    loadServerConfig(),
		CORS:      loadCORSConfig(),
		RateLimit: loadRateLimitConfig(),
	}, nil
}

func loadServerConfig() contracts.ServerConfig {
	return contracts.ServerConfig{
		Host:            getEnv("SERVER_HOST", "0.0.0.0"),
		Port:            getEnv("SERVER_PORT", "8080"),
		ReadTimeout:     getEnvDuration("SERVER_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:    getEnvDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:     getEnvDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func loadCORSConfig() contracts.CORSConfig {
	return contracts.CORSConfig{
		AllowedOrigins:   splitAndTrim(getEnv("CORS_ALLOWED_ORIGINS", "*")),
		AllowedMethods:   splitAndTrim(getEnv("CORS_ALLOWED_METHODS", "GET,POST,PUT,DELETE,OPTIONS")),
		AllowedHeaders:   splitAndTrim(getEnv("CORS_ALLOWED_HEADERS", "Accept,Authorization,Content-Type,X-Request-ID")),
		ExposedHeaders:   splitAndTrim(getEnv("CORS_EXPOSED_HEADERS", "Link")),
		AllowCredentials: getEnvBool("CORS_ALLOW_CREDENTIALS", true),
		MaxAge:           getEnvInt("CORS_MAX_AGE", 300),
	}
}

func loadRateLimitConfig() contracts.RateLimitConfig {
	return contracts.RateLimitConfig{
		Requests: getEnvInt("RATE_LIMIT_REQUESTS", 100),
		Window:   getEnvDuration("RATE_LIMIT_WINDOW", 1*time.Minute),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
