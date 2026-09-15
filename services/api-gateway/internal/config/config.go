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
	Kafka     contracts.KafkaConfig
	Log       contracts.LogConfig
}

func New() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		Server:    loadServerConfig(),
		CORS:      loadCORSConfig(),
		RateLimit: loadRateLimitConfig(),
		Kafka:     loadKafkaConfig(),
		Log:       loadLogConfig(),
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

func loadKafkaConfig() contracts.KafkaConfig {
	return contracts.KafkaConfig{
		Brokers:          splitAndTrim(getEnv("KAFKA_BROKERS", "localhost:9092")),
		WriteTimeout:     getEnvDuration("KAFKA_WRITE_TIMEOUT", 5*time.Second),
		MaxAttempts:      getEnvInt("KAFKA_MAX_ATTEMPTS", 3),
		BreakerThreshold: getEnvUint32("KAFKA_BREAKER_THRESHOLD", 5),
		BreakerTimeout:   getEnvDuration("KAFKA_BREAKER_TIMEOUT", 30*time.Second),
	}
}

func loadLogConfig() contracts.LogConfig {
	return contracts.LogConfig{
		Level:  getEnv("LOG_LEVEL", "info"),
		Pretty: getEnvBool("LOG_PRETTY", false),
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

func getEnvUint32(key string, defaultValue uint32) uint32 {
	value := getEnvInt(key, int(defaultValue))
	if value < 0 {
		return defaultValue
	}
	return uint32(value)
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
