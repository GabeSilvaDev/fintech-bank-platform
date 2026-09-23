package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/internal/contracts"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/response"
)

const (
	maxWebhookBytes = 64 << 10
	maxReasonBytes  = 255
)

type WebhookHandler struct {
	publisher contracts.Publisher
	secret    string
	tolerance time.Duration
	clock     contracts.Clock
	log       *logger.Logger
}

func NewWebhookHandler(publisher contracts.Publisher, secret string, tolerance time.Duration, clock contracts.Clock, log *logger.Logger) *WebhookHandler {
	return &WebhookHandler{publisher: publisher, secret: secret, tolerance: tolerance, clock: clock, log: log}
}

type webhookRequest struct {
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
}

func (h *WebhookHandler) Gateway(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.FromError(w, apperrors.New("PAYLOAD_TOO_LARGE", "request body is too large", http.StatusRequestEntityTooLarge))
			return
		}
		response.FromError(w, apperrors.BadRequest("INVALID_BODY", "request body could not be read").Wrap(err))
		return
	}
	timestamp := r.Header.Get("X-Timestamp")
	if err := services.Verify(h.secret, timestamp, body, r.Header.Get("X-Signature"), h.clock.Now(), h.tolerance); err != nil {
		h.log.Warn().Str("remote_addr", r.RemoteAddr).Str("timestamp", timestamp).Msg("invalid webhook signature")
		response.FromError(w, apperrors.Unauthorized("INVALID_SIGNATURE", "webhook signature is invalid"))
		return
	}

	var req webhookRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.FromError(w, apperrors.BadRequest("INVALID_JSON", "request body is not valid JSON").Wrap(err))
		return
	}
	externalID := strings.TrimSpace(req.ExternalID)
	details := map[string]string{}
	if externalID == "" {
		details["external_id"] = "required"
	}
	if _, ok := models.ParseSettlementStatus(req.Status); !ok {
		details["status"] = "oneof"
	}
	if len(details) > 0 {
		response.FromError(w, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetails(details))
		return
	}

	event := events.NewEvent(events.EventTypes.SettlePayment, "payment-service", events.SettlePaymentPayload{
		ExternalID: externalID,
		Status:     req.Status,
		Reason:     truncate(req.Reason, maxReasonBytes),
	}).WithTraceID(middleware.GetRequestID(r.Context()))
	if err := h.publisher.Publish(r.Context(), events.Topics.PaymentCommands, externalID, event); err != nil {
		response.FromError(w, apperrors.ServiceUnavailable("PUBLISH_FAILED", "settlement could not be queued").Wrap(err))
		return
	}
	response.Accepted(w, map[string]string{"command_id": event.ID})
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
