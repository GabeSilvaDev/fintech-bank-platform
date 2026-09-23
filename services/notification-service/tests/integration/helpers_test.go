//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR must be set")
	}

	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() {
		client.Close()
	})
	return client
}
