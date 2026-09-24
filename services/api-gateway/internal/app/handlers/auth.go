package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/go-playground/validator/v10"
)

const (
	maxAuthBodyBytes = 16 << 10
	minPasswordBytes = 8
	maxPasswordBytes = 72
	bearerTokenType  = "Bearer"
)

type AuthHandler struct {
	identities contracts.IdentityProvider
	tokens     contracts.TokenIssuer
}

func NewAuthHandler(identities contracts.IdentityProvider, tokens contracts.TokenIssuer) *AuthHandler {
	return &AuthHandler{identities: identities, tokens: tokens}
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	UserID      string `json:"user_id,omitempty"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
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

	token, expiresIn, err := h.tokens.Issue(userID)
	if err != nil {
		response.FromError(w, err)
		return
	}

	response.Created(w, tokenResponse{UserID: userID.String(), AccessToken: token, TokenType: bearerTokenType, ExpiresIn: expiresIn})
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

	token, expiresIn, err := h.tokens.Issue(userID)
	if err != nil {
		response.FromError(w, err)
		return
	}

	response.OK(w, tokenResponse{AccessToken: token, TokenType: bearerTokenType, ExpiresIn: expiresIn})
}

func identityFailure(w http.ResponseWriter, err error) {
	if apperrors.GetHTTPStatus(err) == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", bearerTokenType)
	}
	response.FromError(w, err)
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
