package unit

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type stubSessions struct {
	userID    uuid.UUID
	session   services.Session
	err       error
	started   []uuid.UUID
	presented []string
}

func (s *stubSessions) Start(_ context.Context, userID uuid.UUID) (services.Session, error) {
	s.started = append(s.started, userID)
	return s.session, s.err
}

func (s *stubSessions) Rotate(_ context.Context, token string) (uuid.UUID, services.Session, error) {
	s.presented = append(s.presented, token)
	return s.userID, s.session, s.err
}

func (s *stubSessions) Revoke(_ context.Context, token string) error {
	s.presented = append(s.presented, token)
	return s.err
}

func sessionRouter(sessions handlers.SessionManager) http.Handler {
	h := handlers.NewSessionHandler(sessions)
	r := chi.NewRouter()
	r.Post("/sessions", h.Start)
	r.Post("/sessions/rotate", h.Rotate)
	r.Post("/sessions/revoke", h.Revoke)
	return r
}

var sessionExpiry = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestSessionHandlerStartReturnsTheToken(t *testing.T) {
	userID := uuid.New()
	stub := &stubSessions{session: services.Session{Token: "token-value", ExpiresAt: sessionExpiry}}

	rec, body := post(sessionRouter(stub), "/sessions", `{"user_id":"`+userID.String()+`"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, "token-value", data["refresh_token"])
	assert.Equal(t, "2026-10-24T12:00:00Z", data["expires_at"])
	assert.Equal(t, []uuid.UUID{userID}, stub.started)
}

func TestSessionHandlerStartValidatesTheUserID(t *testing.T) {
	stub := &stubSessions{}

	for _, body := range []string{`{}`, `{"user_id":""}`, `{"user_id":"abc"}`, `{"user_id":"00000000-0000-0000-0000-000000000000"}`} {
		rec, decoded := post(sessionRouter(stub), "/sessions", body)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, body)
		errorBody := decoded["error"].(map[string]interface{})
		assert.Equal(t, "VALIDATION_ERROR", errorBody["code"])
		assert.Equal(t, "uuid", errorBody["details"].(map[string]interface{})["user_id"])
	}
	assert.Empty(t, stub.started)
}

func TestSessionHandlerRotateReturnsUserAndToken(t *testing.T) {
	userID := uuid.New()
	stub := &stubSessions{userID: userID, session: services.Session{Token: "next-token", ExpiresAt: sessionExpiry}}

	rec, body := post(sessionRouter(stub), "/sessions/rotate", `{"refresh_token":"old-token"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, userID.String(), data["user_id"])
	assert.Equal(t, "next-token", data["refresh_token"])
	assert.Equal(t, "2026-10-24T12:00:00Z", data["expires_at"])
	assert.Equal(t, []string{"old-token"}, stub.presented)
}

func TestSessionHandlerRotateAnswersInvalidSession(t *testing.T) {
	stub := &stubSessions{err: services.ErrInvalidSession}

	rec, body := post(sessionRouter(stub), "/sessions/rotate", `{"refresh_token":"old-token"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	errorBody := body["error"].(map[string]interface{})
	assert.Equal(t, "INVALID_SESSION", errorBody["code"])
	assert.Equal(t, "refresh token is invalid or expired", errorBody["message"])
	assert.NotContains(t, rec.Body.String(), "old-token")
}

func TestSessionHandlerRevokeAnswersNoContent(t *testing.T) {
	stub := &stubSessions{}

	rec, _ := post(sessionRouter(stub), "/sessions/revoke", `{"refresh_token":"old-token"}`)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, []string{"old-token"}, stub.presented)
}

func TestSessionHandlersHideUnexpectedErrors(t *testing.T) {
	stub := &stubSessions{err: errors.New("cassandra down")}
	requests := map[string]string{
		"/sessions":        `{"user_id":"` + uuid.NewString() + `"}`,
		"/sessions/rotate": `{"refresh_token":"old-token"}`,
		"/sessions/revoke": `{"refresh_token":"old-token"}`,
	}

	for path, body := range requests {
		rec, decoded := post(sessionRouter(stub), path, body)

		assert.Equal(t, http.StatusInternalServerError, rec.Code, path)
		assert.Equal(t, "INTERNAL_ERROR", decoded["error"].(map[string]interface{})["code"])
		assert.NotContains(t, rec.Body.String(), "cassandra")
		assert.NotContains(t, rec.Body.String(), "old-token")
	}
}

func TestSessionHandlersRejectMalformedBodies(t *testing.T) {
	stub := &stubSessions{}
	cases := map[string]string{
		"/sessions":        `{"user_id":"` + uuid.NewString() + `"}`,
		"/sessions/rotate": `{"refresh_token":"old-token"}`,
		"/sessions/revoke": `{"refresh_token":"old-token"}`,
	}

	for path, valid := range cases {
		for _, body := range []string{"", "{", `{"user_id":1,"refresh_token":1}`, strings.TrimSuffix(valid, "}") + `,"extra":true}`, valid + `{}`, valid + ` x`, valid + `}`, valid + ` 1`} {
			rec, decoded := post(sessionRouter(stub), path, body)

			assert.Equal(t, http.StatusBadRequest, rec.Code, path+" "+body)
			assert.Equal(t, "INVALID_JSON", decoded["error"].(map[string]interface{})["code"])
		}
	}
	assert.Empty(t, stub.started)
	assert.Empty(t, stub.presented)
}

func TestSessionHandlersRejectOversizedBodies(t *testing.T) {
	stub := &stubSessions{}
	bodies := map[string]string{
		"/sessions":        `{"user_id":"` + strings.Repeat("a", 16<<10) + `"}`,
		"/sessions/rotate": `{"refresh_token":"` + strings.Repeat("a", 16<<10) + `"}`,
		"/sessions/revoke": `{"refresh_token":"old-token"}` + strings.Repeat(" ", 16<<10),
	}

	for path, body := range bodies {
		rec, decoded := post(sessionRouter(stub), path, body)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, path)
		assert.Equal(t, "PAYLOAD_TOO_LARGE", decoded["error"].(map[string]interface{})["code"])
	}
	assert.Empty(t, stub.presented)
}
