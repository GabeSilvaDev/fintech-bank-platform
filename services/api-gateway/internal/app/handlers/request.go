package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/go-playground/validator/v10"
)

func decode(r *http.Request, dst interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return errors.BadRequest("INVALID_JSON", "request body is not valid JSON").Wrap(err)
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

	return errors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetails(details)
}

func validateID(id string) error {
	if err := validation.ValidateVar(id, "required,uuid"); err != nil {
		return errors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("id", "uuid")
	}

	return nil
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(contracts.RequestIDKey).(string)
	return id
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
