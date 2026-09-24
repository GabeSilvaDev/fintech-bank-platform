package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/identity"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type identityCall struct {
	Method      string
	Path        string
	ContentType string
	RequestID   string
	Body        map[string]string
}

func identityServer(t *testing.T, status int, body string) (*identity.Client, *[]identityCall) {
	t.Helper()
	calls := &[]identityCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := identityCall{
			Method:      r.Method,
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			RequestID:   r.Header.Get(middleware.RequestIDHeader),
		}
		_ = json.NewDecoder(r.Body).Decode(&call.Body)
		*calls = append(*calls, call)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	base, err := url.Parse(server.URL)
	require.NoError(t, err)
	return identity.NewClient(base, time.Second), calls
}

func requestContext(requestID string) context.Context {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, requestID)
	var ctx context.Context
	middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	return ctx
}

func assertAppError(t *testing.T, err error, status int, code string) *apperrors.AppError {
	t.Helper()
	appErr, ok := apperrors.AsAppError(err)
	require.True(t, ok, "expected an AppError, got %v", err)
	assert.Equal(t, status, appErr.HTTPStatus)
	assert.Equal(t, code, appErr.Code)
	return appErr
}

func TestIdentityClientRegisterReturnsTheNewUserID(t *testing.T) {
	userID := uuid.New()
	client, calls := identityServer(t, http.StatusCreated, `{"success":true,"data":{"user_id":"`+userID.String()+`"}}`)

	got, err := client.Register(requestContext("req-register"), "ana@example.com", "s3cret<&>pass")

	require.NoError(t, err)
	assert.Equal(t, userID, got)
	require.Len(t, *calls, 1)
	assert.Equal(t, identityCall{
		Method:      http.MethodPost,
		Path:        "/identities",
		ContentType: "application/json",
		RequestID:   "req-register",
		Body:        map[string]string{"email": "ana@example.com", "password": "s3cret<&>pass"},
	}, (*calls)[0])
}

func TestIdentityClientVerifyReturnsTheUserID(t *testing.T) {
	userID := uuid.New()
	client, calls := identityServer(t, http.StatusOK, `{"success":true,"data":{"user_id":"`+userID.String()+`"}}`)

	got, err := client.Verify(context.Background(), "ana@example.com", "password1")

	require.NoError(t, err)
	assert.Equal(t, userID, got)
	require.Len(t, *calls, 1)
	assert.Equal(t, "/identities/verify", (*calls)[0].Path)
}

func TestIdentityClientMapsUpstreamErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   int
		code   string
	}{
		{"email taken", http.StatusConflict, `{"success":false,"error":{"code":"EMAIL_TAKEN","message":"email already registered"}}`, http.StatusConflict, "EMAIL_TAKEN"},
		{"invalid credentials", http.StatusUnauthorized, `{"success":false,"error":{"code":"INVALID_CREDENTIALS","message":"x"}}`, http.StatusUnauthorized, "INVALID_CREDENTIALS"},
		{"too large", http.StatusRequestEntityTooLarge, `{"success":false}`, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
		{"server error", http.StatusInternalServerError, `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"x"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"bad request", http.StatusBadRequest, `{"success":false,"error":{"code":"INVALID_JSON","message":"x"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"success without json", http.StatusCreated, `not json`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"success without user id", http.StatusCreated, `{"success":true,"data":{}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"success with a bad user id", http.StatusCreated, `{"success":true,"data":{"user_id":"nope"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := identityServer(t, tc.status, tc.body)

			userID, err := client.Register(context.Background(), "ana@example.com", "password1")

			assert.Equal(t, uuid.Nil, userID)
			assertAppError(t, err, tc.want, tc.code)
		})
	}
}

func TestIdentityClientMessages(t *testing.T) {
	client, _ := identityServer(t, http.StatusConflict, `{}`)
	_, err := client.Register(context.Background(), "a@b.co", "password1")
	assert.Equal(t, "e-mail already registered", assertAppError(t, err, http.StatusConflict, "EMAIL_TAKEN").Message)

	client, _ = identityServer(t, http.StatusUnauthorized, `{}`)
	_, err = client.Verify(context.Background(), "a@b.co", "password1")
	assert.Equal(t, "invalid e-mail or password", assertAppError(t, err, http.StatusUnauthorized, "INVALID_CREDENTIALS").Message)

	client, _ = identityServer(t, http.StatusBadGateway, `{}`)
	_, err = client.Verify(context.Background(), "a@b.co", "password1")
	assert.Equal(t, "account service is unavailable", assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE").Message)
}

func TestIdentityClientPassesValidationDetailsThrough(t *testing.T) {
	client, _ := identityServer(t, http.StatusUnprocessableEntity, `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"request validation failed","details":{"password":"length"}}}`)

	_, err := client.Register(context.Background(), "ana@example.com", "short")

	appErr := assertAppError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	assert.Equal(t, map[string]string{"password": "length"}, appErr.Details)
}

func TestIdentityClientReportsAnUnreachableService(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")

	_, err := identity.NewClient(base, time.Second).Verify(context.Background(), "ana@example.com", "password1")

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
}

func TestIdentityClientGivesUpAfterTheTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	base, _ := url.Parse(server.URL)

	start := time.Now()
	_, err := identity.NewClient(base, 50*time.Millisecond).Verify(context.Background(), "ana@example.com", "password1")

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestIdentityClientRejectsARequestItCannotBuild(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")
	var missing context.Context

	_, err := identity.NewClient(base, time.Second).Register(missing, "ana@example.com", "password1")

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
}
