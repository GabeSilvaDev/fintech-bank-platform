package config

import (
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/env"
	"github.com/joho/godotenv"
)

type Config struct {
	Server    contracts.ServerConfig
	CORS      contracts.CORSConfig
	RateLimit contracts.RateLimitConfig
	Kafka     contracts.KafkaConfig
	Log       contracts.LogConfig
	Upstreams contracts.UpstreamConfig
}

func New() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		Server:    loadServerConfig(),
		CORS:      loadCORSConfig(),
		RateLimit: loadRateLimitConfig(),
		Kafka:     loadKafkaConfig(),
		Log:       loadLogConfig(),
		Upstreams: contracts.UpstreamConfig{AccountService: env.Get("ACCOUNT_SERVICE_URL", "http://localhost:8082")},
	}, nil
}

func loadServerConfig() contracts.ServerConfig {
	return contracts.ServerConfig{
		Host:            env.Get("SERVER_HOST", "0.0.0.0"),
		Port:            env.Get("SERVER_PORT", "8080"),
		ReadTimeout:     env.GetDuration("SERVER_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:    env.GetDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:     env.GetDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: env.GetDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func loadCORSConfig() contracts.CORSConfig {
	return contracts.CORSConfig{
		AllowedOrigins:   env.SplitAndTrim(env.Get("CORS_ALLOWED_ORIGINS", "*")),
		AllowedMethods:   env.SplitAndTrim(env.Get("CORS_ALLOWED_METHODS", "GET,POST,PUT,DELETE,OPTIONS")),
		AllowedHeaders:   env.SplitAndTrim(env.Get("CORS_ALLOWED_HEADERS", "Accept,Authorization,Content-Type,X-Request-ID")),
		ExposedHeaders:   env.SplitAndTrim(env.Get("CORS_EXPOSED_HEADERS", "Link")),
		AllowCredentials: env.GetBool("CORS_ALLOW_CREDENTIALS", true),
		MaxAge:           env.GetInt("CORS_MAX_AGE", 300),
	}
}

func loadRateLimitConfig() contracts.RateLimitConfig {
	return contracts.RateLimitConfig{
		Requests: env.GetInt("RATE_LIMIT_REQUESTS", 100),
		Window:   env.GetDuration("RATE_LIMIT_WINDOW", 1*time.Minute),
	}
}

func loadKafkaConfig() contracts.KafkaConfig {
	return contracts.KafkaConfig{
		Brokers:          env.SplitAndTrim(env.Get("KAFKA_BROKERS", "localhost:9092")),
		WriteTimeout:     env.GetDuration("KAFKA_WRITE_TIMEOUT", 5*time.Second),
		BatchTimeout:     env.GetDuration("KAFKA_BATCH_TIMEOUT", 10*time.Millisecond),
		PublishTimeout:   env.GetDuration("KAFKA_PUBLISH_TIMEOUT", 20*time.Second),
		MaxAttempts:      env.GetIntMin("KAFKA_MAX_ATTEMPTS", 3, 1),
		BreakerThreshold: env.GetUint32("KAFKA_BREAKER_THRESHOLD", 5),
		BreakerTimeout:   env.GetDuration("KAFKA_BREAKER_TIMEOUT", 30*time.Second),
	}
}

func loadLogConfig() contracts.LogConfig {
	return contracts.LogConfig{
		Level:  env.Get("LOG_LEVEL", "info"),
		Pretty: env.GetBool("LOG_PRETTY", false),
	}
}
