package unit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
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
		for _, body := range []string{"", "{", `{"email":1}`, `{"email":"a@b.com","password":"12345678","extra":true}`} {
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

func TestIdentityHandlersHideUnexpectedErrors(t *testing.T) {
	router := identityRouter(stubIdentities{err: errors.New("cassandra down")})

	for _, path := range []string{"/identities", "/identities/verify"} {
		rec, decoded := post(router, path, `{"email":"ana@example.com","password":"correct horse"}`)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Equal(t, "INTERNAL_ERROR", decoded["error"].(map[string]interface{})["code"])
		assert.NotContains(t, rec.Body.String(), "cassandra")
	}
}
