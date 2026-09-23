package unit

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8085", cfg.Server.Address())
	assert.Equal(t, 30*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.Server.WriteTimeout)
	assert.Equal(t, 120*time.Second, cfg.Server.IdleTimeout)
	assert.Equal(t, 10*time.Second, cfg.Server.ShutdownTimeout)
	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "notification-service", cfg.Kafka.GroupID)
	assert.Equal(t, 5*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 10*time.Millisecond, cfg.Kafka.BatchTimeout)
	assert.Equal(t, 20*time.Second, cfg.Kafka.PublishTimeout)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 30*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, time.Hour, cfg.Consumer.MaxEventAge)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.False(t, cfg.Log.Pretty)
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
	assert.Equal(t, "", cfg.Redis.Password)
	assert.Equal(t, 0, cfg.Redis.DB)
	assert.Equal(t, "http://localhost:8082", cfg.Directory.URL)
	assert.Equal(t, 5*time.Minute, cfg.Directory.TTL)
	assert.Equal(t, 3*time.Second, cfg.Directory.Timeout)
	assert.Equal(t, "localhost:1025", cfg.SMTP.Addr)
	assert.Equal(t, "no-reply@fintech.local", cfg.SMTP.From)
	assert.Equal(t, 10*time.Second, cfg.SMTP.Timeout)
	assert.Equal(t, 100, cfg.HistorySize)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("KAFKA_GROUP_ID", "notif-test")
	t.Setenv("CONSUMER_RETRY_BACKOFF", "10ms,20ms")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("REDIS_ADDR", "r:1")
	t.Setenv("REDIS_PASSWORD", "pw")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("ACCOUNT_SERVICE_URL", "http://acc")
	t.Setenv("ACCOUNT_DIRECTORY_TTL", "1m")
	t.Setenv("ACCOUNT_DIRECTORY_TIMEOUT", "1s")
	t.Setenv("SMTP_ADDR", "m:25")
	t.Setenv("SMTP_FROM", "a@b.c")
	t.Setenv("SMTP_TIMEOUT", "2s")
	t.Setenv("NOTIFICATION_HISTORY_SIZE", "0")
	t.Setenv("NOTIFICATION_MAX_EVENT_AGE", "0")
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "5")
	t.Setenv("STARTUP_RETRY_DELAY", "500ms")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9000", cfg.Server.Address())
	assert.Equal(t, "notif-test", cfg.Kafka.GroupID)
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "r:1", cfg.Redis.Addr)
	assert.Equal(t, "pw", cfg.Redis.Password)
	assert.Equal(t, 2, cfg.Redis.DB)
	assert.Equal(t, "http://acc", cfg.Directory.URL)
	assert.Equal(t, time.Minute, cfg.Directory.TTL)
	assert.Equal(t, time.Second, cfg.Directory.Timeout)
	assert.Equal(t, "m:25", cfg.SMTP.Addr)
	assert.Equal(t, "a@b.c", cfg.SMTP.From)
	assert.Equal(t, 2*time.Second, cfg.SMTP.Timeout)
	assert.Equal(t, 100, cfg.HistorySize)
	assert.Equal(t, time.Duration(0), cfg.Consumer.MaxEventAge)
	assert.Equal(t, 5, cfg.Startup.Attempts)
	assert.Equal(t, 500*time.Millisecond, cfg.Startup.Delay)
}

func TestConfigStartupInvalidValuesFallBackToDefaults(t *testing.T) {
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "0")
	t.Setenv("STARTUP_RETRY_DELAY", "-1s")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}

func TestConfigObservabilityDefaults(t *testing.T) {
	cfg, err := config.New()

	assert.NoError(t, err)
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
