package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
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
	return identityServerWithHeaders(t, status, body, nil)
}

func identityServerWithHeaders(t *testing.T, status int, body string, headers map[string]string) (*identity.Client, *[]identityCall) {
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
		for name, value := range headers {
			w.Header().Set(name, value)
		}
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
		{"busy", http.StatusServiceUnavailable, `{"success":false,"error":{"code":"SERVICE_BUSY","message":"x"}}`, http.StatusServiceUnavailable, "SERVICE_BUSY"},
		{"redirect", http.StatusTemporaryRedirect, `{"success":true,"data":{"user_id":"` + uuid.NewString() + `"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
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

func TestIdentityClientDoesNotFollowRedirects(t *testing.T) {
	followed := false
	mux := http.NewServeMux()
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
		followed = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"user_id":"` + uuid.NewString() + `"}}`))
	})
	mux.HandleFunc("/identities", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	base, _ := url.Parse(server.URL)

	userID, err := identity.NewClient(base, time.Second).Register(context.Background(), "ana@example.com", "password1")

	assert.Equal(t, uuid.Nil, userID)
	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	assert.False(t, followed)
}

func TestIdentityClientBusyMessage(t *testing.T) {
	client, _ := identityServer(t, http.StatusServiceUnavailable, `{}`)

	_, err := client.Verify(context.Background(), "ana@example.com", "password1")

	assert.Equal(t, "account service is busy, try again shortly", assertAppError(t, err, http.StatusServiceUnavailable, "SERVICE_BUSY").Message)
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

func retryAfterOf(err error) string {
	var retryAfter contracts.RetryAfter
	if errors.As(err, &retryAfter) {
		return string(retryAfter)
	}
	return ""
}

func TestIdentityClientMapsALockoutAndForwardsRetryAfter(t *testing.T) {
	client, _ := identityServerWithHeaders(t, http.StatusTooManyRequests, `{"success":false,"error":{"code":"TOO_MANY_ATTEMPTS","message":"x"}}`, map[string]string{"Retry-After": "840"})

	_, err := client.Verify(context.Background(), "ana@example.com", "password1")

	appErr := assertAppError(t, err, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS")
	assert.Equal(t, "too many failed attempts; try again later", appErr.Message)
	assert.Equal(t, "840", retryAfterOf(err))
	assert.Equal(t, "retry after 840 seconds", contracts.RetryAfter("840").Error())
}

func TestIdentityClientForwardsRetryAfterWhenBusy(t *testing.T) {
	client, _ := identityServerWithHeaders(t, http.StatusServiceUnavailable, `{"success":false,"error":{"code":"SERVICE_BUSY","message":"x"}}`, map[string]string{"Retry-After": "1"})

	_, err := client.Register(context.Background(), "ana@example.com", "password1")

	assertAppError(t, err, http.StatusServiceUnavailable, "SERVICE_BUSY")
	assert.Equal(t, "1", retryAfterOf(err))
}

func TestIdentityClientIgnoresAMissingOrInvalidRetryAfter(t *testing.T) {
	for _, value := range []string{"", "soon", "-1", "Wed, 21 Oct 2026 07:28:00 GMT", "99999999999"} {
		client, _ := identityServerWithHeaders(t, http.StatusTooManyRequests, `{}`, map[string]string{"Retry-After": value})

		_, err := client.Verify(context.Background(), "ana@example.com", "password1")

		assertAppError(t, err, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS")
		assert.Empty(t, retryAfterOf(err), value)
	}
}

func TestIdentityClientNormalisesRetryAfter(t *testing.T) {
	client, _ := identityServerWithHeaders(t, http.StatusTooManyRequests, `{}`, map[string]string{"Retry-After": "007"})

	_, err := client.Verify(context.Background(), "ana@example.com", "password1")

	assert.Equal(t, "7", retryAfterOf(err))
}

func TestIdentityClientStartsASession(t *testing.T) {
	userID := uuid.New()
	client, calls := identityServer(t, http.StatusCreated, `{"success":true,"data":{"refresh_token":"refresh-1","expires_at":"2026-10-24T12:00:00Z"}}`)

	session, err := client.StartSession(requestContext("req-session"), userID)

	require.NoError(t, err)
	assert.Equal(t, contracts.Session{RefreshToken: "refresh-1", ExpiresAt: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}, session)
	require.Len(t, *calls, 1)
	assert.Equal(t, identityCall{
		Method:      http.MethodPost,
		Path:        "/sessions",
		ContentType: "application/json",
		RequestID:   "req-session",
		Body:        map[string]string{"user_id": userID.String()},
	}, (*calls)[0])
}

func TestIdentityClientReportsAFailedSessionStart(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   int
		code   string
	}{
		{"validation", http.StatusUnprocessableEntity, `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"x"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"server error", http.StatusInternalServerError, `{}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"busy", http.StatusServiceUnavailable, `{}`, http.StatusServiceUnavailable, "SERVICE_BUSY"},
		{"not json", http.StatusCreated, `nope`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"no token", http.StatusCreated, `{"success":true,"data":{"expires_at":"2026-10-24T12:00:00Z"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"no expiry", http.StatusCreated, `{"success":true,"data":{"refresh_token":"refresh-1"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"bad expiry", http.StatusCreated, `{"success":true,"data":{"refresh_token":"refresh-1","expires_at":"later"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := identityServer(t, tc.status, tc.body)

			session, err := client.StartSession(context.Background(), uuid.New())

			assert.Equal(t, contracts.Session{}, session)
			assertAppError(t, err, tc.want, tc.code)
		})
	}
}

func TestIdentityClientRotatesASession(t *testing.T) {
	userID := uuid.New()
	client, calls := identityServer(t, http.StatusOK, `{"success":true,"data":{"user_id":"`+userID.String()+`","refresh_token":"refresh-2","expires_at":"2026-10-24T12:00:00Z"}}`)

	got, session, err := client.RotateSession(context.Background(), "refresh-1")

	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, contracts.Session{RefreshToken: "refresh-2", ExpiresAt: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}, session)
	require.Len(t, *calls, 1)
	assert.Equal(t, "/sessions/rotate", (*calls)[0].Path)
	assert.Equal(t, map[string]string{"refresh_token": "refresh-1"}, (*calls)[0].Body)
}

func TestIdentityClientReportsAFailedRotation(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   int
		code   string
	}{
		{"invalid session", http.StatusUnauthorized, `{"success":false,"error":{"code":"INVALID_SESSION","message":"x"}}`, http.StatusUnauthorized, "INVALID_SESSION"},
		{"validation", http.StatusUnprocessableEntity, `{}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"server error", http.StatusInternalServerError, `{}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"busy", http.StatusServiceUnavailable, `{}`, http.StatusServiceUnavailable, "SERVICE_BUSY"},
		{"not json", http.StatusOK, `nope`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"bad user id", http.StatusOK, `{"success":true,"data":{"user_id":"nope","refresh_token":"refresh-2","expires_at":"2026-10-24T12:00:00Z"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
		{"no token", http.StatusOK, `{"success":true,"data":{"user_id":"` + uuid.NewString() + `","expires_at":"2026-10-24T12:00:00Z"}}`, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := identityServer(t, tc.status, tc.body)

			userID, session, err := client.RotateSession(context.Background(), "refresh-1")

			assert.Equal(t, uuid.Nil, userID)
			assert.Equal(t, contracts.Session{}, session)
			assertAppError(t, err, tc.want, tc.code)
		})
	}
}

func TestIdentityClientInvalidSessionMessage(t *testing.T) {
	client, _ := identityServer(t, http.StatusUnauthorized, `{}`)

	_, _, err := client.RotateSession(context.Background(), "refresh-1")

	assert.Equal(t, "refresh token is invalid or expired", assertAppError(t, err, http.StatusUnauthorized, "INVALID_SESSION").Message)
}

func TestIdentityClientRevokesASession(t *testing.T) {
	client, calls := identityServer(t, http.StatusNoContent, ``)

	require.NoError(t, client.RevokeSession(context.Background(), "refresh-1"))
	require.Len(t, *calls, 1)
	assert.Equal(t, "/sessions/revoke", (*calls)[0].Path)
	assert.Equal(t, map[string]string{"refresh_token": "refresh-1"}, (*calls)[0].Body)
}

func TestIdentityClientTreatsAnUnknownSessionAsRevoked(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity} {
		client, _ := identityServer(t, status, `{"success":false,"error":{"code":"INVALID_SESSION","message":"x"}}`)

		assert.NoError(t, client.RevokeSession(context.Background(), "refresh-1"), status)
	}
}

func TestIdentityClientReportsAFailedRevocation(t *testing.T) {
	client, _ := identityServer(t, http.StatusInternalServerError, `{}`)
	assertAppError(t, client.RevokeSession(context.Background(), "refresh-1"), http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")

	client, _ = identityServer(t, http.StatusServiceUnavailable, `{}`)
	assertAppError(t, client.RevokeSession(context.Background(), "refresh-1"), http.StatusServiceUnavailable, "SERVICE_BUSY")
}

func TestIdentityClientSessionCallsReportAnUnreachableService(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")
	client := identity.NewClient(base, time.Second)

	_, err := client.StartSession(context.Background(), uuid.New())
	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")

	_, _, err = client.RotateSession(context.Background(), "refresh-1")
	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")

	assertAppError(t, client.RevokeSession(context.Background(), "refresh-1"), http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
}
