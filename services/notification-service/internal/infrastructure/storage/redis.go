package storage

import (
	"context"

	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/redis/go-redis/v9"
)

func NewClient(cfg contracts.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB})
}

func Ping(client *redis.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}
}
