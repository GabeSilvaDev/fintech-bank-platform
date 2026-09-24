package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"

	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/google/uuid"
)

const maxBodyBytes = 16 << 10

var errTrailingData = errors.New("unexpected data after the JSON object")

type IdentityManager interface {
	Register(ctx context.Context, email, password string) (uuid.UUID, error)
	Verify(ctx context.Context, email, password string) (uuid.UUID, error)
}

type IdentityHandler struct {
	identities IdentityManager
}

func NewIdentityHandler(identities IdentityManager) *IdentityHandler {
	return &IdentityHandler{identities: identities}
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type identityResponse struct {
	UserID string `json:"user_id"`
}

func (h *IdentityHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := decodeBody(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}

	userID, err := h.identities.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		writeIdentityError(w, err)
		return
	}

	response.Created(w, identityResponse{UserID: userID.String()})
}

func (h *IdentityHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := decodeBody(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}

	userID, err := h.identities.Verify(r.Context(), req.Email, req.Password)
	if err != nil {
		writeIdentityError(w, err)
		return
	}

	response.OK(w, identityResponse{UserID: userID.String()})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(dst)
	if err == nil {
		err = rejectTrailing(decoder)
	}
	if err == nil {
		return nil
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return apperrors.New("PAYLOAD_TOO_LARGE", "request body exceeds 16 KiB", http.StatusRequestEntityTooLarge)
	}
	return apperrors.BadRequest("INVALID_JSON", "request body is not valid JSON")
}

func rejectTrailing(decoder *json.Decoder) error {
	_, err := decoder.Token()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errTrailingData
	}
	return err
}

func writeIdentityError(w http.ResponseWriter, err error) {
	var locked *services.ErrTooManyAttempts
	switch {
	case errors.As(err, &locked):
		seconds := max(1, int64(math.Ceil(locked.RetryAfter.Seconds())))
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		response.FromError(w, apperrors.TooManyRequests("TOO_MANY_ATTEMPTS", "too many failed attempts; try again later"))
		return
	case errors.Is(err, services.ErrBusy):
		w.Header().Set("Retry-After", "1")
	}
	response.FromError(w, mapIdentityError(err))
}

func mapIdentityError(err error) error {
	switch {
	case errors.Is(err, services.ErrEmailTaken):
		return apperrors.Conflict("EMAIL_TAKEN", "email already registered")
	case errors.Is(err, services.ErrInvalidCredentials):
		return apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	case errors.Is(err, services.ErrBusy):
		return apperrors.ServiceUnavailable("SERVICE_BUSY", "service is busy, try again shortly")
	case domain.InvalidCode(err) == "invalid_email":
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("email", "email")
	case domain.InvalidCode(err) == "invalid_password":
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("password", "length")
	}
	return err
}
