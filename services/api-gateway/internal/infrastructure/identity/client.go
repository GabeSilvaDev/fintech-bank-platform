package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/google/uuid"
)

const maxResponseBytes = 64 << 10

type Client struct {
	base *url.URL
	http *http.Client
}

func NewClient(base *url.URL, timeout time.Duration) *Client {
	return &Client{
		base: base,
		http: &http.Client{Transport: tracing.Transport(nil), Timeout: timeout},
	}
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type envelope struct {
	Data struct {
		UserID string `json:"user_id"`
	} `json:"data"`
	Error struct {
		Details map[string]string `json:"details"`
	} `json:"error"`
}

func (c *Client) Register(ctx context.Context, email, password string) (uuid.UUID, error) {
	return c.call(ctx, "identities", http.StatusCreated, email, password)
}

func (c *Client) Verify(ctx context.Context, email, password string) (uuid.UUID, error) {
	return c.call(ctx, "identities/verify", http.StatusOK, email, password)
}

func (c *Client) call(ctx context.Context, path string, success int, email, password string) (uuid.UUID, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(credentials{Email: email, Password: password})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath(path).String(), &body)
	if err != nil {
		return uuid.Nil, unavailable(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.RequestIDHeader, middleware.GetRequestID(ctx))

	resp, err := c.http.Do(req)
	if err != nil {
		return uuid.Nil, unavailable(err)
	}
	defer resp.Body.Close()

	var payload envelope
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&payload)

	switch resp.StatusCode {
	case success:
		userID, err := uuid.Parse(payload.Data.UserID)
		if decodeErr != nil || err != nil {
			return uuid.Nil, unavailable(fmt.Errorf("unexpected identity response: %v %v", decodeErr, err))
		}
		return userID, nil
	case http.StatusConflict:
		return uuid.Nil, apperrors.Conflict("EMAIL_TAKEN", "e-mail already registered")
	case http.StatusUnauthorized:
		return uuid.Nil, apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid e-mail or password")
	case http.StatusUnprocessableEntity:
		return uuid.Nil, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetails(payload.Error.Details)
	case http.StatusRequestEntityTooLarge:
		return uuid.Nil, apperrors.New("PAYLOAD_TOO_LARGE", "request body is too large", http.StatusRequestEntityTooLarge)
	}
	return uuid.Nil, unavailable(fmt.Errorf("unexpected identity status %d", resp.StatusCode))
}

func unavailable(err error) error {
	return apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway).Wrap(err)
}
