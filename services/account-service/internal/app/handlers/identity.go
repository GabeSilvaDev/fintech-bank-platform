package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/google/uuid"
)

const maxIdentityBodyBytes = 16 << 10

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
	if err := decodeCredentials(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}

	userID, err := h.identities.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		response.FromError(w, mapIdentityError(err))
		return
	}

	response.Created(w, identityResponse{UserID: userID.String()})
}

func (h *IdentityHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := decodeCredentials(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}

	userID, err := h.identities.Verify(r.Context(), req.Email, req.Password)
	if err != nil {
		response.FromError(w, mapIdentityError(err))
		return
	}

	response.OK(w, identityResponse{UserID: userID.String()})
}

func decodeCredentials(w http.ResponseWriter, r *http.Request, dst *credentialsRequest) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxIdentityBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apperrors.New("PAYLOAD_TOO_LARGE", "request body exceeds 16 KiB", http.StatusRequestEntityTooLarge)
		}
		return apperrors.BadRequest("INVALID_JSON", "request body is not valid JSON")
	}
	return nil
}

func mapIdentityError(err error) error {
	switch {
	case errors.Is(err, services.ErrEmailTaken):
		return apperrors.Conflict("EMAIL_TAKEN", "email already registered")
	case errors.Is(err, services.ErrInvalidCredentials):
		return apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	case domain.InvalidCode(err) == "invalid_email":
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("email", "email")
	case domain.InvalidCode(err) == "invalid_password":
		return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("password", "length")
	}
	return err
}
