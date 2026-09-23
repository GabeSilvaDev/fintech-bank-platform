package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type History struct {
	client *redis.Client
	size   int
}

func NewHistory(client *redis.Client, size int) *History {
	return &History{client: client, size: size}
}

type storedRecord struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	Channel       string    `json:"channel"`
	Recipient     string    `json:"recipient"`
	Subject       string    `json:"subject"`
	Body          string    `json:"body"`
	SourceEventID string    `json:"source_event_id"`
	SentAt        time.Time `json:"sent_at"`
}

func historyKey(userID uuid.UUID) string {
	return "notification:history:" + userID.String()
}

func (h *History) Append(ctx context.Context, record models.Record) error {
	raw, err := json.Marshal(storedRecord{
		ID:            record.ID,
		UserID:        record.UserID.String(),
		Channel:       string(record.Channel),
		Recipient:     record.Recipient,
		Subject:       record.Subject,
		Body:          record.Body,
		SourceEventID: record.SourceEventID,
		SentAt:        record.SentAt,
	})
	if err != nil {
		return err
	}
	key := historyKey(record.UserID)
	pipe := h.client.TxPipeline()
	pipe.LPush(ctx, key, raw)
	pipe.LTrim(ctx, key, 0, int64(h.size-1))
	_, err = pipe.Exec(ctx)
	return err
}

func (h *History) List(ctx context.Context, userID uuid.UUID, limit int) ([]models.Record, error) {
	values, err := h.client.LRange(ctx, historyKey(userID), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	records := make([]models.Record, 0, len(values))
	for _, value := range values {
		var stored storedRecord
		if err := json.Unmarshal([]byte(value), &stored); err != nil {
			return nil, err
		}
		records = append(records, models.Record{
			ID:            stored.ID,
			UserID:        userID,
			Channel:       models.Channel(stored.Channel),
			Recipient:     stored.Recipient,
			Subject:       stored.Subject,
			Body:          stored.Body,
			SourceEventID: stored.SourceEventID,
			SentAt:        stored.SentAt,
		})
	}
	return records, nil
}
