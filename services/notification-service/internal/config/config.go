package config

import (
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/env"
	"github.com/joho/godotenv"
)

type Config struct {
	Server      contracts.ServerConfig
	Kafka       contracts.KafkaConfig
	Consumer    contracts.ConsumerConfig
	Log         contracts.LogConfig
	Redis       contracts.RedisConfig
	Directory   contracts.DirectoryConfig
	SMTP        contracts.SMTPConfig
	HistorySize int
}

func New() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		Server: contracts.ServerConfig{
			Host:            env.Get("SERVER_HOST", "0.0.0.0"),
			Port:            env.Get("SERVER_PORT", "8085"),
			ReadTimeout:     env.GetDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    env.GetDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     env.GetDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: env.GetDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Kafka: contracts.KafkaConfig{
			Brokers:        env.SplitAndTrim(env.Get("KAFKA_BROKERS", "localhost:9092")),
			GroupID:        env.Get("KAFKA_GROUP_ID", "notification-service"),
			WriteTimeout:   env.GetDuration("KAFKA_WRITE_TIMEOUT", 5*time.Second),
			BatchTimeout:   env.GetDuration("KAFKA_BATCH_TIMEOUT", 10*time.Millisecond),
			PublishTimeout: env.GetDuration("KAFKA_PUBLISH_TIMEOUT", 20*time.Second),
			MaxAttempts:    env.GetIntMin("KAFKA_MAX_ATTEMPTS", 3, 1),
		},
		Consumer: contracts.ConsumerConfig{
			RetryBackoff: env.GetDurations("CONSUMER_RETRY_BACKOFF", []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}),
			DrainTimeout: env.GetDuration("CONSUMER_DRAIN_TIMEOUT", 30*time.Second),
		},
		Log: contracts.LogConfig{
			Level:  env.Get("LOG_LEVEL", "info"),
			Pretty: env.GetBool("LOG_PRETTY", false),
		},
		Redis: contracts.RedisConfig{
			Addr:     env.Get("REDIS_ADDR", "localhost:6379"),
			Password: env.Get("REDIS_PASSWORD", ""),
			DB:       env.GetInt("REDIS_DB", 0),
		},
		Directory: contracts.DirectoryConfig{
			URL:     env.Get("ACCOUNT_SERVICE_URL", "http://localhost:8082"),
			TTL:     env.GetDuration("ACCOUNT_DIRECTORY_TTL", 5*time.Minute),
			Timeout: env.GetDuration("ACCOUNT_DIRECTORY_TIMEOUT", 3*time.Second),
		},
		SMTP: contracts.SMTPConfig{
			Addr: env.Get("SMTP_ADDR", "localhost:1025"),
			From: env.Get("SMTP_FROM", "no-reply@fintech.local"),
		},
		HistorySize: env.GetIntMin("NOTIFICATION_HISTORY_SIZE", 100, 1),
	}, nil
}
