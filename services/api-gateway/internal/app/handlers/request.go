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

const maxAmountCents = 999999999999999

func decode(r *http.Request, dst interface{}) error {
	return decodeLimited(r, dst, maxBodyBytes, "request body exceeds 1 MiB")
}

func decodeLimited(r *http.Request, dst interface{}, limit int64, tooLargeMessage string) error {
	r.Body = http.MaxBytesReader(nil, r.Body, limit)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apperrors.New("PAYLOAD_TOO_LARGE", tooLargeMessage, http.StatusRequestEntityTooLarge).Wrap(err)
		}
		if domain.InvalidCode(err) == "invalid_amount" {
			return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("amount", "amount")
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

func validateAmount(amount domain.Amount) error {
	if amount.Cents() <= 0 {
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("amount", "gt")
	}
	if amount.Cents() > maxAmountCents {
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("amount", "lte")
	}

	return nil
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
