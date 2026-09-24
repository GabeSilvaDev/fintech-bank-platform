package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/go-playground/validator/v10"
)

const maxBodyBytes = 1 << 20

func decode(r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apperrors.New("PAYLOAD_TOO_LARGE", "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge).Wrap(err)
		}
		return apperrors.BadRequest("INVALID_JSON", "request body is not valid JSON").Wrap(err)
	}

	return nil
}

func validate(dst interface{}) error {
	err := validation.Validate(dst)
	if err == nil {
		return nil
	}

	details := make(map[string]string)
	if fieldErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range fieldErrors {
			details[fe.Field()] = fe.Tag()
		}
	}

	return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetails(details)
}

func amountOf(value float64) (domain.Amount, error) {
	cents, err := domain.ToCents(value)
	if err != nil {
		return 0, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("amount", "amount")
	}

	return domain.AmountFromCents(cents), nil
}

func validateID(id string) error {
	if err := validation.ValidateVar(id, "required,uuid"); err != nil {
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("id", "uuid")
	}

	return nil
}

func requestID(r *http.Request) string {
	return middleware.GetRequestID(r.Context())
}

func publish(w http.ResponseWriter, r *http.Request, pub contracts.Publisher, topic, key string, event *events.Event) {
	event.WithTraceID(requestID(r))

	if err := pub.Publish(r.Context(), topic, key, event); err != nil {
		response.FromError(w, err)
		return
	}

	response.Accepted(w, map[string]string{
		"command_id": event.ID,
		"trace_id":   event.TraceID,
	})
}
