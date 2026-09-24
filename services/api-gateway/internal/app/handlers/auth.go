package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

const (
	maxAuthBodyBytes     = 16 << 10
	minPasswordBytes     = 8
	maxPasswordBytes     = 72
	maxRefreshTokenBytes = 256
	bearerTokenType      = "Bearer"
)

type AuthHandler struct {
	identities contracts.IdentityProvider
	sessions   contracts.SessionProvider
	tokens     contracts.TokenIssuer
}

func NewAuthHandler(identities contracts.IdentityProvider, sessions contracts.SessionProvider, tokens contracts.TokenIssuer) *AuthHandler {
	return &AuthHandler{identities: identities, sessions: sessions, tokens: tokens}
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	UserID           string `json:"user_id,omitempty"`
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	req, err := decodeCredentials(r, registrationDetails)
	if err != nil {
		response.FromError(w, err)
		return
	}

	userID, err := h.identities.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		identityFailure(w, err)
		return
	}

	h.startSession(w, r, http.StatusCreated, userID, true)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	req, err := decodeCredentials(r, loginDetails)
	if err != nil {
		response.FromError(w, err)
		return
	}

	userID, err := h.identities.Verify(r.Context(), req.Email, req.Password)
	if err != nil {
		identityFailure(w, err)
		return
	}

	h.startSession(w, r, http.StatusOK, userID, false)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := decodeRefreshToken(r)
	if err != nil {
		response.FromError(w, err)
		return
	}
	if len(refreshToken) > maxRefreshTokenBytes {
		identityFailure(w, apperrors.Unauthorized("INVALID_SESSION", "refresh token is invalid or expired"))
		return
	}

	userID, session, err := h.sessions.RotateSession(r.Context(), refreshToken)
	if err != nil {
		identityFailure(w, err)
		return
	}

	h.respondWithTokens(w, http.StatusOK, userID, session, false)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := decodeRefreshToken(r)
	if err != nil {
		response.FromError(w, err)
		return
	}
	if len(refreshToken) > maxRefreshTokenBytes {
		response.NoContent(w)
		return
	}

	if err := h.sessions.RevokeSession(r.Context(), refreshToken); err != nil {
		identityFailure(w, err)
		return
	}

	response.NoContent(w)
}

func (h *AuthHandler) startSession(w http.ResponseWriter, r *http.Request, status int, userID uuid.UUID, withUserID bool) {
	session, err := h.sessions.StartSession(r.Context(), userID)
	if err != nil {
		identityFailure(w, err)
		return
	}

	h.respondWithTokens(w, status, userID, session, withUserID)
}

func (h *AuthHandler) respondWithTokens(w http.ResponseWriter, status int, userID uuid.UUID, session contracts.Session, withUserID bool) {
	token, expiresIn, err := h.tokens.Issue(userID)
	if err != nil {
		response.FromError(w, err)
		return
	}

	body := tokenResponse{
		AccessToken:      token,
		TokenType:        bearerTokenType,
		ExpiresIn:        expiresIn,
		RefreshToken:     session.RefreshToken,
		RefreshExpiresIn: secondsUntil(session.ExpiresAt),
	}
	if withUserID {
		body.UserID = userID.String()
	}
	response.Success(w, status, body)
}

func secondsUntil(t time.Time) int {
	if seconds := int(time.Until(t) / time.Second); seconds > 0 {
		return seconds
	}
	return 0
}

func identityFailure(w http.ResponseWriter, err error) {
	switch apperrors.GetHTTPStatus(err) {
	case http.StatusUnauthorized:
		w.Header().Set("WWW-Authenticate", bearerTokenType)
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		var retryAfter contracts.RetryAfter
		if errors.As(err, &retryAfter) {
			w.Header().Set("Retry-After", string(retryAfter))
		}
	}
	response.FromError(w, err)
}

func decodeRefreshToken(r *http.Request) (string, error) {
	var req refreshTokenRequest
	if err := decodeLimited(r, &req, maxAuthBodyBytes, "request body exceeds 16 KiB"); err != nil {
		return "", err
	}
	if req.RefreshToken == "" {
		return "", apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("refresh_token", "required")
	}
	return req.RefreshToken, nil
}

func decodeCredentials(r *http.Request, rules func(credentialsRequest) map[string]string) (credentialsRequest, error) {
	var req credentialsRequest
	if err := decodeLimited(r, &req, maxAuthBodyBytes, "request body exceeds 16 KiB"); err != nil {
		return req, err
	}

	req.Email = strings.TrimSpace(req.Email)
	if details := rules(req); len(details) > 0 {
		return req, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetails(details)
	}
	return req, nil
}

func registrationDetails(req credentialsRequest) map[string]string {
	details := make(map[string]string)
	if rule := brokenRule(req.Email, "required,email,max=254"); rule != "" {
		details["email"] = rule
	}
	switch {
	case req.Password == "":
		details["password"] = "required"
	case len(req.Password) < minPasswordBytes || len(req.Password) > maxPasswordBytes:
		details["password"] = "length"
	}
	return details
}

func loginDetails(req credentialsRequest) map[string]string {
	details := make(map[string]string)
	if req.Email == "" {
		details["email"] = "required"
	}
	if req.Password == "" {
		details["password"] = "required"
	}
	return details
}

func brokenRule(value interface{}, rules string) string {
	var fieldErrors validator.ValidationErrors
	if errors.As(validation.ValidateVar(value, rules), &fieldErrors) {
		return fieldErrors[0].Tag()
	}
	return ""
}
