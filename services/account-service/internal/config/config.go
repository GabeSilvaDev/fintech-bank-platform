package config

import (
	"runtime"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/env"
	"github.com/joho/godotenv"
)

type Config struct {
	Server        contracts.ServerConfig
	Kafka         contracts.KafkaConfig
	Cassandra     contracts.CassandraConfig
	Consumer      contracts.ConsumerConfig
	Log           contracts.LogConfig
	Startup       contracts.StartupConfig
	Observability contracts.ObservabilityConfig
	Identity      contracts.IdentityConfig
}

func New() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		Server: contracts.ServerConfig{
			Host:            env.Get("SERVER_HOST", "0.0.0.0"),
			Port:            env.Get("SERVER_PORT", "8082"),
			ReadTimeout:     env.GetDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    env.GetDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     env.GetDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: env.GetDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Kafka: contracts.KafkaConfig{
			Brokers:        env.SplitAndTrim(env.Get("KAFKA_BROKERS", "localhost:9092")),
			GroupID:        env.Get("KAFKA_GROUP_ID", "account-service"),
			WriteTimeout:   env.GetDuration("KAFKA_WRITE_TIMEOUT", 5*time.Second),
			BatchTimeout:   env.GetDuration("KAFKA_BATCH_TIMEOUT", 10*time.Millisecond),
			PublishTimeout: env.GetDuration("KAFKA_PUBLISH_TIMEOUT", 20*time.Second),
			MaxAttempts:    env.GetIntMin("KAFKA_MAX_ATTEMPTS", 3, 1),
		},
		Cassandra: contracts.CassandraConfig{
			Hosts:          env.SplitAndTrim(env.Get("CASSANDRA_HOSTS", "localhost:9042")),
			Keyspace:       env.Get("CASSANDRA_KEYSPACE", "fintech_accounts"),
			Consistency:    env.Get("CASSANDRA_CONSISTENCY", "LOCAL_QUORUM"),
			Timeout:        env.GetDuration("CASSANDRA_TIMEOUT", 10*time.Second),
			ConnectTimeout: env.GetDuration("CASSANDRA_CONNECT_TIMEOUT", 10*time.Second),
			MigrationsPath: env.Get("CASSANDRA_MIGRATIONS_PATH", "migrations"),
		},
		Consumer: contracts.ConsumerConfig{
			RetryBackoff: env.GetDurations("CONSUMER_RETRY_BACKOFF", []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}),
			DrainTimeout: env.GetDuration("CONSUMER_DRAIN_TIMEOUT", 30*time.Second),
		},
		Log: contracts.LogConfig{
			Level:  env.Get("LOG_LEVEL", "info"),
			Pretty: env.GetBool("LOG_PRETTY", false),
		},
		Startup: contracts.StartupConfig{
			Attempts: env.GetIntMin("STARTUP_RETRY_ATTEMPTS", 30, 1),
			Delay:    positiveDuration(env.GetDuration("STARTUP_RETRY_DELAY", 2*time.Second), 2*time.Second),
		},
		Observability: loadObservabilityConfig(),
		Identity: contracts.IdentityConfig{
			HashConcurrency: env.GetIntMin("IDENTITY_HASH_CONCURRENCY", 2*runtime.GOMAXPROCS(0), 1),
		},
	}, nil
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func loadObservabilityConfig() contracts.ObservabilityConfig {
	return contracts.ObservabilityConfig{
		MetricsEnabled: env.GetBool("METRICS_ENABLED", true),
		OTLPEndpoint:   env.Get("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		SampleRatio:    env.GetFloat("OTEL_SAMPLER_RATIO", 1.0),
	}
}
