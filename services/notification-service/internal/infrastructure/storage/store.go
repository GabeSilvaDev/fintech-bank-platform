package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const processedTTL = 7 * 24 * time.Hour

type Store struct {
	client *redis.Client
}

func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

func (s *Store) MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error) {
	return s.client.SetNX(ctx, "notification:processed:"+eventID.String(), 1, processedTTL).Result()
}
