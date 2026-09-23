package config

import (
	"errors"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/env"
	"github.com/joho/godotenv"
)

type Config struct {
	Server    contracts.ServerConfig
	Kafka     contracts.KafkaConfig
	Cassandra contracts.CassandraConfig
	Consumer  contracts.ConsumerConfig
	Log       contracts.LogConfig
	Payment   contracts.PaymentConfig
}

func New() (*Config, error) {
	_ = godotenv.Load()

	payment := contracts.PaymentConfig{
		WebhookSecret:    env.Get("PAYMENT_WEBHOOK_SECRET", ""),
		WebhookURL:       env.Get("PAYMENT_WEBHOOK_URL", "http://localhost:8084/webhooks/gateway"),
		WebhookTolerance: env.GetDuration("PAYMENT_WEBHOOK_TOLERANCE", 5*time.Minute),
		SettlementDelay:  env.GetDuration("PAYMENT_SETTLEMENT_DELAY", 2*time.Second),
	}
	if payment.WebhookSecret == "" {
		return nil, errors.New("PAYMENT_WEBHOOK_SECRET is required")
	}

	return &Config{
		Server: contracts.ServerConfig{
			Host:            env.Get("SERVER_HOST", "0.0.0.0"),
			Port:            env.Get("SERVER_PORT", "8084"),
			ReadTimeout:     env.GetDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    env.GetDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     env.GetDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: env.GetDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Kafka: contracts.KafkaConfig{
			Brokers:        env.SplitAndTrim(env.Get("KAFKA_BROKERS", "localhost:9092")),
			GroupID:        env.Get("KAFKA_GROUP_ID", "payment-service"),
			WriteTimeout:   env.GetDuration("KAFKA_WRITE_TIMEOUT", 5*time.Second),
			BatchTimeout:   env.GetDuration("KAFKA_BATCH_TIMEOUT", 10*time.Millisecond),
			PublishTimeout: env.GetDuration("KAFKA_PUBLISH_TIMEOUT", 20*time.Second),
			MaxAttempts:    env.GetIntMin("KAFKA_MAX_ATTEMPTS", 3, 1),
		},
		Cassandra: contracts.CassandraConfig{
			Hosts:          env.SplitAndTrim(env.Get("CASSANDRA_HOSTS", "localhost:9042")),
			Keyspace:       env.Get("CASSANDRA_KEYSPACE", "fintech_payments"),
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
		Payment: payment,
	}, nil
}
