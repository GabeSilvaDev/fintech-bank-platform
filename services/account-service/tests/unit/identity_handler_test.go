package unit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type stubIdentities struct {
	userID uuid.UUID
	err    error
}

func (s stubIdentities) Register(context.Context, string, string) (uuid.UUID, error) {
	return s.userID, s.err
}

func (s stubIdentities) Verify(context.Context, string, string) (uuid.UUID, error) {
	return s.userID, s.err
}

func identityRouter(identities handlers.IdentityManager) http.Handler {
	h := handlers.NewIdentityHandler(identities)
	r := chi.NewRouter()
	r.Post("/identities", h.Register)
	r.Post("/identities/verify", h.Verify)
	return r
}

func post(handler http.Handler, path, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, tests.FromJson(rec.Body.String())
}

func TestIdentityHandlersReturnUserID(t *testing.T) {
	userID := uuid.New()
	router := identityRouter(stubIdentities{userID: userID})

	rec, body := post(router, "/identities", `{"email":"ana@example.com","password":"correct horse"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, userID.String(), body["data"].(map[string]interface{})["user_id"])

	rec, body = post(router, "/identities/verify", `{"email":"ana@example.com","password":"correct horse"}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, userID.String(), body["data"].(map[string]interface{})["user_id"])
}

func TestIdentityHandlersRejectMalformedBodies(t *testing.T) {
	router := identityRouter(stubIdentities{userID: uuid.New()})
	for _, path := range []string{"/identities", "/identities/verify"} {
		for _, body := range []string{"", "{", `{"email":1}`, `{"email":"a@b.com","password":"12345678","extra":true}`, `{"email":"a@b.com","password":"12345678"}{}`, `{"email":"a@b.com","password":"12345678"} x`, `{"email":"a@b.com","password":"12345678"}}`, `{"email":"a@b.com","password":"12345678"} 1`} {
			rec, decoded := post(router, path, body)

			assert.Equal(t, http.StatusBadRequest, rec.Code, body)
			assert.Equal(t, "INVALID_JSON", decoded["error"].(map[string]interface{})["code"])
		}
	}
}

func TestIdentityHandlersRejectOversizedBodies(t *testing.T) {
	router := identityRouter(stubIdentities{userID: uuid.New()})
	body := `{"email":"ana@example.com","password":"` + strings.Repeat("a", 16<<10) + `"}`

	for _, path := range []string{"/identities", "/identities/verify"} {
		rec, decoded := post(router, path, body)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		assert.Equal(t, "PAYLOAD_TOO_LARGE", decoded["error"].(map[string]interface{})["code"])
	}
}

func TestIdentityHandlersAcceptTrailingWhitespace(t *testing.T) {
	router := identityRouter(stubIdentities{userID: uuid.New()})

	rec, _ := post(router, "/identities/verify", "{\"email\":\"a@b.com\",\"password\":\"12345678\"}\n  \n")

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestIdentityHandlersRejectOversizedTrailingData(t *testing.T) {
	router := identityRouter(stubIdentities{userID: uuid.New()})
	body := `{"email":"ana@example.com","password":"correct horse"}` + strings.Repeat(" ", 16<<10)

	rec, decoded := post(router, "/identities", body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "PAYLOAD_TOO_LARGE", decoded["error"].(map[string]interface{})["code"])
}

func TestIdentityHandlersHideUnexpectedErrors(t *testing.T) {
	router := identityRouter(stubIdentities{err: errors.New("cassandra down")})

	for _, path := range []string{"/identities", "/identities/verify"} {
		rec, decoded := post(router, path, `{"email":"ana@example.com","password":"correct horse"}`)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Equal(t, "INTERNAL_ERROR", decoded["error"].(map[string]interface{})["code"])
		assert.NotContains(t, rec.Body.String(), "cassandra")
	}
}

func TestIdentityHandlersAnswerBusyWhenHashingIsSaturated(t *testing.T) {
	router := identityRouter(stubIdentities{err: services.ErrBusy})

	for _, path := range []string{"/identities", "/identities/verify"} {
		rec, decoded := post(router, path, `{"email":"ana@example.com","password":"correct horse"}`)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		errorBody := decoded["error"].(map[string]interface{})
		assert.Equal(t, "SERVICE_BUSY", errorBody["code"])
		assert.Equal(t, "service is busy, try again shortly", errorBody["message"])
		assert.Equal(t, "1", rec.Header().Get("Retry-After"))
	}
}

func TestIdentityHandlersAnswerTooManyAttemptsWithRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		retryAfter time.Duration
		want       string
	}{
		{15 * time.Minute, "900"},
		{1500 * time.Millisecond, "2"},
		{time.Second, "1"},
		{0, "1"},
	} {
		router := identityRouter(stubIdentities{err: fmt.Errorf("wrapped: %w", &services.ErrTooManyAttempts{RetryAfter: tc.retryAfter})})

		rec, decoded := post(router, "/identities/verify", `{"email":"ana@example.com","password":"correct horse"}`)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		errorBody := decoded["error"].(map[string]interface{})
		assert.Equal(t, "TOO_MANY_ATTEMPTS", errorBody["code"])
		assert.Equal(t, "too many failed attempts; try again later", errorBody["message"])
		assert.Equal(t, tc.want, rec.Header().Get("Retry-After"), tc.retryAfter)
	}
}

func TestIdentityHandlersOnlySendRetryAfterWhenRetryingHelps(t *testing.T) {
	for _, err := range []error{services.ErrInvalidCredentials, services.ErrEmailTaken, errors.New("cassandra down")} {
		router := identityRouter(stubIdentities{err: err})

		for _, path := range []string{"/identities", "/identities/verify"} {
			rec, _ := post(router, path, `{"email":"ana@example.com","password":"correct horse"}`)

			assert.Empty(t, rec.Header().Get("Retry-After"), err.Error())
		}
	}
}
