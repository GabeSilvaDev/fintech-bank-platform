package database

import (
	"context"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

type ProcessedEventStore struct {
	session *gocql.Session
}

func NewProcessedEventStore(session *gocql.Session) *ProcessedEventStore {
	return &ProcessedEventStore{session: session}
}

func (s *ProcessedEventStore) MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error) {
	return s.session.Query("INSERT INTO processed_events (event_id, processed_at) VALUES (?, ?) IF NOT EXISTS", gocql.UUID(eventID), time.Now().UTC()).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
}
