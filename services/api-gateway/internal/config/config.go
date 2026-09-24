package config

import (
	"errors"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/env"
	"github.com/joho/godotenv"
)

type Config struct {
	Server        contracts.ServerConfig
	CORS          contracts.CORSConfig
	RateLimit     contracts.RateLimitConfig
	Kafka         contracts.KafkaConfig
	Log           contracts.LogConfig
	Upstreams     contracts.UpstreamConfig
	Observability contracts.ObservabilityConfig
	Auth          contracts.AuthConfig
	AuthRateLimit contracts.RateLimitConfig
}

const minJWTSecretBytes = 32

var ErrWeakJWTSecret = errors.New("JWT_SECRET must be set to at least 32 bytes")

func New() (*Config, error) {
	_ = godotenv.Load()

	auth, err := loadAuthConfig()
	if err != nil {
		return nil, err
	}

	return &Config{
		Server:        loadServerConfig(),
		CORS:          loadCORSConfig(),
		RateLimit:     loadRateLimitConfig(),
		Kafka:         loadKafkaConfig(),
		Log:           loadLogConfig(),
		Observability: loadObservabilityConfig(),
		Auth:          auth,
		AuthRateLimit: loadAuthRateLimitConfig(),
		Upstreams: contracts.UpstreamConfig{
			AccountService:      env.Get("ACCOUNT_SERVICE_URL", "http://localhost:8082"),
			TransactionService:  env.Get("TRANSACTION_SERVICE_URL", "http://localhost:8083"),
			PaymentService:      env.Get("PAYMENT_SERVICE_URL", "http://localhost:8084"),
			NotificationService: env.Get("NOTIFICATION_SERVICE_URL", "http://localhost:8085"),
		},
	}, nil
}

func loadServerConfig() contracts.ServerConfig {
	return contracts.ServerConfig{
		Host:              env.Get("SERVER_HOST", "0.0.0.0"),
		Port:              env.Get("SERVER_PORT", "8080"),
		ReadTimeout:       env.GetDuration("SERVER_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:      env.GetDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:       env.GetDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:   env.GetDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
		TrustProxyHeaders: env.GetBool("TRUST_PROXY_HEADERS", false),
		TrustedProxyHops:  env.GetIntMin("TRUSTED_PROXY_HOPS", 1, 1),
	}
}

func loadCORSConfig() contracts.CORSConfig {
	return contracts.CORSConfig{
		AllowedOrigins:   env.SplitAndTrim(env.Get("CORS_ALLOWED_ORIGINS", "*")),
		AllowedMethods:   env.SplitAndTrim(env.Get("CORS_ALLOWED_METHODS", "GET,POST,PUT,DELETE,OPTIONS")),
		AllowedHeaders:   env.SplitAndTrim(env.Get("CORS_ALLOWED_HEADERS", "Accept,Authorization,Content-Type,X-Request-ID")),
		ExposedHeaders:   env.SplitAndTrim(env.Get("CORS_EXPOSED_HEADERS", "Link,X-Next-Before")),
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

func loadObservabilityConfig() contracts.ObservabilityConfig {
	return contracts.ObservabilityConfig{
		MetricsEnabled: env.GetBool("METRICS_ENABLED", true),
		OTLPEndpoint:   env.Get("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		SampleRatio:    env.GetFloat("OTEL_SAMPLER_RATIO", 1.0),
	}
}

func loadAuthConfig() (contracts.AuthConfig, error) {
	secret := env.Get("JWT_SECRET", "")
	if len(secret) < minJWTSecretBytes {
		return contracts.AuthConfig{}, ErrWeakJWTSecret
	}

	return contracts.AuthConfig{
		JWTSecret:     secret,
		TokenTTL:      positiveDuration("JWT_TTL", time.Hour),
		OwnerCacheTTL: positiveDuration("OWNER_CACHE_TTL", time.Minute),
	}, nil
}

func loadAuthRateLimitConfig() contracts.RateLimitConfig {
	return contracts.RateLimitConfig{
		Requests: env.GetIntMin("AUTH_RATE_LIMIT_REQUESTS", 10, 1),
		Window:   positiveDuration("AUTH_RATE_LIMIT_WINDOW", time.Minute),
	}
}

func positiveDuration(key string, defaultValue time.Duration) time.Duration {
	if value := env.GetDuration(key, defaultValue); value > 0 {
		return value
	}
	return defaultValue
}
