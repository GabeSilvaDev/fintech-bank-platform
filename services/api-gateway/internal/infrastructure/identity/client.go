package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
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
		http: &http.Client{
			Transport: tracing.Transport(nil),
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionStart struct {
	UserID string `json:"user_id"`
}

type sessionToken struct {
	RefreshToken string `json:"refresh_token"`
}

type envelope struct {
	Data struct {
		UserID       string    `json:"user_id"`
		RefreshToken string    `json:"refresh_token"`
		ExpiresAt    time.Time `json:"expires_at"`
	} `json:"data"`
	Error struct {
		Details map[string]string `json:"details"`
	} `json:"error"`
}

type reply struct {
	status     int
	retryAfter string
	payload    envelope
	decodeErr  error
}

func (c *Client) Register(ctx context.Context, email, password string) (uuid.UUID, error) {
	return c.identity(ctx, "identities", http.StatusCreated, email, password)
}

func (c *Client) Verify(ctx context.Context, email, password string) (uuid.UUID, error) {
	return c.identity(ctx, "identities/verify", http.StatusOK, email, password)
}

func (c *Client) StartSession(ctx context.Context, userID uuid.UUID) (contracts.Session, error) {
	answer, err := c.post(ctx, "sessions", sessionStart{UserID: userID.String()})
	if err != nil {
		return contracts.Session{}, err
	}
	if answer.status != http.StatusCreated {
		return contracts.Session{}, failure(answer)
	}
	return session(answer)
}

func (c *Client) RotateSession(ctx context.Context, refreshToken string) (uuid.UUID, contracts.Session, error) {
	answer, err := c.post(ctx, "sessions/rotate", sessionToken{RefreshToken: refreshToken})
	if err != nil {
		return uuid.Nil, contracts.Session{}, err
	}
	switch answer.status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return uuid.Nil, contracts.Session{}, apperrors.Unauthorized("INVALID_SESSION", "refresh token is invalid or expired")
	default:
		return uuid.Nil, contracts.Session{}, failure(answer)
	}

	userID, err := answer.userID()
	if err != nil {
		return uuid.Nil, contracts.Session{}, err
	}
	rotated, err := session(answer)
	if err != nil {
		return uuid.Nil, contracts.Session{}, err
	}
	return userID, rotated, nil
}

func (c *Client) RevokeSession(ctx context.Context, refreshToken string) error {
	answer, err := c.post(ctx, "sessions/revoke", sessionToken{RefreshToken: refreshToken})
	if err != nil {
		return err
	}
	switch answer.status {
	case http.StatusNoContent, http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity:
		return nil
	}
	return failure(answer)
}

func (c *Client) identity(ctx context.Context, path string, success int, email, password string) (uuid.UUID, error) {
	answer, err := c.post(ctx, path, credentials{Email: email, Password: password})
	if err != nil {
		return uuid.Nil, err
	}

	switch answer.status {
	case success:
		return answer.userID()
	case http.StatusConflict:
		return uuid.Nil, apperrors.Conflict("EMAIL_TAKEN", "e-mail already registered")
	case http.StatusUnauthorized:
		return uuid.Nil, apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid e-mail or password")
	case http.StatusUnprocessableEntity:
		return uuid.Nil, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetails(answer.payload.Error.Details)
	case http.StatusRequestEntityTooLarge:
		return uuid.Nil, apperrors.New("PAYLOAD_TOO_LARGE", "request body is too large", http.StatusRequestEntityTooLarge)
	case http.StatusTooManyRequests:
		return uuid.Nil, withRetryAfter(apperrors.TooManyRequests("TOO_MANY_ATTEMPTS", "too many failed attempts; try again later"), answer)
	}
	return uuid.Nil, failure(answer)
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) (reply, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath(path).String(), &body)
	if err != nil {
		return reply{}, unavailable(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.RequestIDHeader, middleware.GetRequestID(ctx))

	resp, err := c.http.Do(req)
	if err != nil {
		return reply{}, unavailable(err)
	}
	defer drain(resp.Body)

	answer := reply{status: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After")}
	if resp.StatusCode != http.StatusNoContent {
		answer.decodeErr = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer.payload)
	}
	return answer, nil
}

func (r reply) userID() (uuid.UUID, error) {
	userID, err := uuid.Parse(r.payload.Data.UserID)
	if r.decodeErr != nil || err != nil {
		return uuid.Nil, unavailable(fmt.Errorf("unexpected account service response: %v %v", r.decodeErr, err))
	}
	return userID, nil
}

func session(r reply) (contracts.Session, error) {
	data := r.payload.Data
	if r.decodeErr != nil || data.RefreshToken == "" || data.ExpiresAt.IsZero() {
		return contracts.Session{}, unavailable(fmt.Errorf("unexpected session response: %v", r.decodeErr))
	}
	return contracts.Session{RefreshToken: data.RefreshToken, ExpiresAt: data.ExpiresAt}, nil
}

func failure(r reply) error {
	if r.status == http.StatusServiceUnavailable {
		return withRetryAfter(apperrors.ServiceUnavailable("SERVICE_BUSY", "account service is busy, try again shortly"), r)
	}
	return unavailable(fmt.Errorf("unexpected account service status %d", r.status))
}

func withRetryAfter(err *apperrors.AppError, r reply) *apperrors.AppError {
	if seconds, parseErr := strconv.ParseUint(r.retryAfter, 10, 32); parseErr == nil {
		return err.Wrap(contracts.RetryAfter(strconv.FormatUint(seconds, 10)))
	}
	return err
}

func drain(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseBytes))
	_ = body.Close()
}

func unavailable(err error) error {
	return apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway).Wrap(err)
}
