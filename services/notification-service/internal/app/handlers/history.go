package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type HistoryReader interface {
	List(ctx context.Context, userID uuid.UUID, limit int) ([]models.Record, error)
}

type HistoryHandler struct {
	history HistoryReader
}

func NewHistoryHandler(history HistoryReader) *HistoryHandler {
	return &HistoryHandler{history: history}
}

type notificationResponse struct {
	ID            string    `json:"id"`
	Channel       string    `json:"channel"`
	Recipient     string    `json:"recipient"`
	Subject       string    `json:"subject,omitempty"`
	Body          string    `json:"body"`
	SourceEventID string    `json:"source_event_id,omitempty"`
	SentAt        time.Time `json:"sent_at"`
}

func (h *HistoryHandler) ListUserNotifications(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		response.FromError(w, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("user_id", "uuid"))
		return
	}
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxLimit {
			response.FromError(w, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("limit", "range"))
			return
		}
	}
	records, err := h.history.List(r.Context(), userID, limit)
	if err != nil {
		response.FromError(w, err)
		return
	}
	items := make([]notificationResponse, 0, len(records))
	for _, record := range records {
		items = append(items, notificationResponse{
			ID:            record.ID,
			Channel:       string(record.Channel),
			Recipient:     models.MaskRecipient(record.Channel, record.Recipient),
			Subject:       record.Subject,
			Body:          record.Body,
			SourceEventID: record.SourceEventID,
			SentAt:        record.SentAt,
		})
	}
	response.OK(w, items)
}
