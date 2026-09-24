package unit

import (
	"os"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/stretchr/testify/assert"
)

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

func TestConfigServerTrustProxyHeadersDefault(t *testing.T) {
	os.Unsetenv("TRUST_PROXY_HEADERS")

	cfg, _ := config.New()

	assert.False(t, cfg.Server.TrustProxyHeaders)
}

func TestConfigServerTrustProxyHeadersFromEnv(t *testing.T) {
	os.Setenv("TRUST_PROXY_HEADERS", "true")
	defer os.Unsetenv("TRUST_PROXY_HEADERS")

	cfg, _ := config.New()

	assert.True(t, cfg.Server.TrustProxyHeaders)
}

func TestConfigServerTrustedProxyHopsDefault(t *testing.T) {
	os.Unsetenv("TRUSTED_PROXY_HOPS")

	cfg, _ := config.New()

	assert.Equal(t, 1, cfg.Server.TrustedProxyHops)
}

func TestConfigServerTrustedProxyHopsFromEnv(t *testing.T) {
	os.Setenv("TRUSTED_PROXY_HOPS", "3")
	defer os.Unsetenv("TRUSTED_PROXY_HOPS")

	cfg, _ := config.New()

	assert.Equal(t, 3, cfg.Server.TrustedProxyHops)
}

func TestConfigServerTrustedProxyHopsZeroFallsBack(t *testing.T) {
	os.Setenv("TRUSTED_PROXY_HOPS", "0")
	defer os.Unsetenv("TRUSTED_PROXY_HOPS")

	cfg, _ := config.New()

	assert.Equal(t, 1, cfg.Server.TrustedProxyHops)
}

func TestConfigServerTrustedProxyHopsNegativeFallsBack(t *testing.T) {
	os.Setenv("TRUSTED_PROXY_HOPS", "-1")
	defer os.Unsetenv("TRUSTED_PROXY_HOPS")

	cfg, _ := config.New()

	assert.Equal(t, 1, cfg.Server.TrustedProxyHops)
}

func TestConfigCORSWithEnvVars(t *testing.T) {
	os.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")
	os.Setenv("CORS_ALLOW_CREDENTIALS", "true")
	os.Setenv("CORS_MAX_AGE", "600")
	defer func() {
		os.Unsetenv("CORS_ALLOWED_ORIGINS")
		os.Unsetenv("CORS_ALLOW_CREDENTIALS")
		os.Unsetenv("CORS_MAX_AGE")
	}()

	cfg, _ := config.New()

	assert.Len(t, cfg.CORS.AllowedOrigins, 2)
	assert.True(t, cfg.CORS.AllowCredentials)
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
	t.Setenv("CORS_EXPOSED_HEADERS", "")
	os.Unsetenv("CORS_EXPOSED_HEADERS")

	cfg, _ := config.New()

	assert.NotEmpty(t, cfg.Server.Host)
	assert.NotEmpty(t, cfg.Server.Port)
	assert.Equal(t, []string{"*"}, cfg.CORS.AllowedOrigins)
	assert.Equal(t, []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, cfg.CORS.AllowedMethods)
	assert.False(t, cfg.CORS.AllowCredentials)
	assert.Equal(t, []string{"Link", "X-Next-Before", "Retry-After"}, cfg.CORS.ExposedHeaders)
	assert.Greater(t, cfg.RateLimit.Requests, 0)
	assert.Greater(t, cfg.RateLimit.Window, time.Duration(0))
}

func TestConfigKafkaDefaults(t *testing.T) {
	cfg, _ := config.New()

	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, 5*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 10*time.Millisecond, cfg.Kafka.BatchTimeout)
	assert.Equal(t, 20*time.Second, cfg.Kafka.PublishTimeout)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, uint32(5), cfg.Kafka.BreakerThreshold)
	assert.Equal(t, 30*time.Second, cfg.Kafka.BreakerTimeout)
}

func TestConfigKafkaWithEnvVars(t *testing.T) {
	os.Setenv("KAFKA_BROKERS", "kafka-1:9092, kafka-2:9092")
	os.Setenv("KAFKA_WRITE_TIMEOUT", "2s")
	os.Setenv("KAFKA_BATCH_TIMEOUT", "50ms")
	os.Setenv("KAFKA_PUBLISH_TIMEOUT", "7s")
	os.Setenv("KAFKA_MAX_ATTEMPTS", "7")
	os.Setenv("KAFKA_BREAKER_THRESHOLD", "9")
	os.Setenv("KAFKA_BREAKER_TIMEOUT", "1m")
	defer func() {
		os.Unsetenv("KAFKA_BROKERS")
		os.Unsetenv("KAFKA_WRITE_TIMEOUT")
		os.Unsetenv("KAFKA_BATCH_TIMEOUT")
		os.Unsetenv("KAFKA_PUBLISH_TIMEOUT")
		os.Unsetenv("KAFKA_MAX_ATTEMPTS")
		os.Unsetenv("KAFKA_BREAKER_THRESHOLD")
		os.Unsetenv("KAFKA_BREAKER_TIMEOUT")
	}()

	cfg, _ := config.New()

	assert.Equal(t, []string{"kafka-1:9092", "kafka-2:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, 2*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 50*time.Millisecond, cfg.Kafka.BatchTimeout)
	assert.Equal(t, 7*time.Second, cfg.Kafka.PublishTimeout)
	assert.Equal(t, 7, cfg.Kafka.MaxAttempts)
	assert.Equal(t, uint32(9), cfg.Kafka.BreakerThreshold)
	assert.Equal(t, time.Minute, cfg.Kafka.BreakerTimeout)
}

func TestConfigLogDefaults(t *testing.T) {
	cfg, _ := config.New()

	assert.Equal(t, "info", cfg.Log.Level)
	assert.False(t, cfg.Log.Pretty)
}

func TestConfigLogWithEnvVars(t *testing.T) {
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_PRETTY", "true")
	defer func() {
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("LOG_PRETTY")
	}()

	cfg, _ := config.New()

	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Log.Pretty)
}

func TestConfigKafkaNegativeBreakerThresholdFallsBack(t *testing.T) {
	os.Setenv("KAFKA_BREAKER_THRESHOLD", "-1")
	defer os.Unsetenv("KAFKA_BREAKER_THRESHOLD")

	cfg, _ := config.New()

	assert.Equal(t, uint32(5), cfg.Kafka.BreakerThreshold)
}

func TestConfigKafkaZeroBreakerThresholdFallsBack(t *testing.T) {
	os.Setenv("KAFKA_BREAKER_THRESHOLD", "0")
	defer os.Unsetenv("KAFKA_BREAKER_THRESHOLD")

	cfg, _ := config.New()

	assert.Equal(t, uint32(5), cfg.Kafka.BreakerThreshold)
}

func TestConfigKafkaZeroMaxAttemptsFallsBack(t *testing.T) {
	os.Setenv("KAFKA_MAX_ATTEMPTS", "0")
	defer os.Unsetenv("KAFKA_MAX_ATTEMPTS")

	cfg, _ := config.New()

	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
}

func TestConfigUpstreamDefaults(t *testing.T) {
	cfg, _ := config.New()
	assert.Equal(t, "http://localhost:8082", cfg.Upstreams.AccountService)
	assert.Equal(t, "http://localhost:8083", cfg.Upstreams.TransactionService)
	assert.Equal(t, "http://localhost:8084", cfg.Upstreams.PaymentService)
	assert.Equal(t, "http://localhost:8085", cfg.Upstreams.NotificationService)
}

func TestConfigObservabilityDefaults(t *testing.T) {
	cfg, _ := config.New()

	assert.True(t, cfg.Observability.MetricsEnabled)
	assert.Empty(t, cfg.Observability.OTLPEndpoint)
	assert.Equal(t, 1.0, cfg.Observability.SampleRatio)
}

func TestConfigObservabilityFromEnv(t *testing.T) {
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SAMPLER_RATIO", "0.5")

	cfg, _ := config.New()

	assert.False(t, cfg.Observability.MetricsEnabled)
	assert.Equal(t, "http://collector:4318", cfg.Observability.OTLPEndpoint)
	assert.Equal(t, 0.5, cfg.Observability.SampleRatio)
}

func TestConfigObservabilityInvalidSampleRatioFallsBack(t *testing.T) {
	t.Setenv("OTEL_SAMPLER_RATIO", "not-a-number")

	cfg, _ := config.New()

	assert.Equal(t, 1.0, cfg.Observability.SampleRatio)
}

func TestConfigUpstreamFromEnv(t *testing.T) {
	t.Setenv("ACCOUNT_SERVICE_URL", "http://accounts:9000")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://txns:9000")
	t.Setenv("PAYMENT_SERVICE_URL", "http://pay:9000")
	t.Setenv("NOTIFICATION_SERVICE_URL", "http://notify:9000")
	cfg, _ := config.New()
	assert.Equal(t, "http://accounts:9000", cfg.Upstreams.AccountService)
	assert.Equal(t, "http://txns:9000", cfg.Upstreams.TransactionService)
	assert.Equal(t, "http://pay:9000", cfg.Upstreams.PaymentService)
	assert.Equal(t, "http://notify:9000", cfg.Upstreams.NotificationService)
}
