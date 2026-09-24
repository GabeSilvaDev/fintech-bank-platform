package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/services"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/google/uuid"
)

type SessionManager interface {
	Start(ctx context.Context, userID uuid.UUID) (services.Session, error)
	Rotate(ctx context.Context, token string) (uuid.UUID, services.Session, error)
	Revoke(ctx context.Context, token string) error
}

type SessionHandler struct {
	sessions SessionManager
}

func NewSessionHandler(sessions SessionManager) *SessionHandler {
	return &SessionHandler{sessions: sessions}
}

type startSessionRequest struct {
	UserID string `json:"user_id"`
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type sessionResponse struct {
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type rotatedSessionResponse struct {
	UserID       string    `json:"user_id"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (h *SessionHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req startSessionRequest
	if err := decodeBody(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil || userID == uuid.Nil {
		response.FromError(w, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("user_id", "uuid"))
		return
	}

	session, err := h.sessions.Start(r.Context(), userID)
	if err != nil {
		response.FromError(w, err)
		return
	}

	response.Created(w, sessionResponse{RefreshToken: session.Token, ExpiresAt: session.ExpiresAt})
}

func (h *SessionHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if err := decodeBody(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}

	userID, session, err := h.sessions.Rotate(r.Context(), req.RefreshToken)
	if err != nil {
		response.FromError(w, mapSessionError(err))
		return
	}

	response.OK(w, rotatedSessionResponse{UserID: userID.String(), RefreshToken: session.Token, ExpiresAt: session.ExpiresAt})
}

func (h *SessionHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if err := decodeBody(w, r, &req); err != nil {
		response.FromError(w, err)
		return
	}

	if err := h.sessions.Revoke(r.Context(), req.RefreshToken); err != nil {
		response.FromError(w, err)
		return
	}

	response.NoContent(w)
}

func mapSessionError(err error) error {
	if errors.Is(err, services.ErrInvalidSession) {
		return apperrors.Unauthorized("INVALID_SESSION", "refresh token is invalid or expired")
	}
	return err
}
