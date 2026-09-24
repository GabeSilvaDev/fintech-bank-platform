package unit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIdentities struct {
	userID uuid.UUID
	err    error
	calls  []string
	emails []string
}

func (f *fakeIdentities) Register(_ context.Context, email, _ string) (uuid.UUID, error) {
	f.calls = append(f.calls, "register")
	f.emails = append(f.emails, email)
	return f.userID, f.err
}

func (f *fakeIdentities) Verify(_ context.Context, email, _ string) (uuid.UUID, error) {
	f.calls = append(f.calls, "verify")
	f.emails = append(f.emails, email)
	return f.userID, f.err
}

type fakeTokens struct {
	err    error
	issued []uuid.UUID
}

func (f *fakeTokens) Issue(userID uuid.UUID) (string, int, error) {
	f.issued = append(f.issued, userID)
	return "signed-token", 3600, f.err
}

func callAuth(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestAuthHandlerRegisterReturnsATokenForTheNewUser(t *testing.T) {
	userID := uuid.New()
	identities := &fakeIdentities{userID: userID}
	tokens := &fakeTokens{}

	rec := callAuth(handlers.NewAuthHandler(identities, tokens).Register, `{"email":"  Ana@Example.com ","password":"password1"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"user_id":"`+userID.String()+`","access_token":"signed-token","token_type":"Bearer","expires_in":3600}}`, rec.Body.String())
	assert.Equal(t, []string{"register"}, identities.calls)
	assert.Equal(t, []string{"Ana@Example.com"}, identities.emails)
	assert.Equal(t, []uuid.UUID{userID}, tokens.issued)
}

func TestAuthHandlerRegisterValidatesBeforeCallingTheAccountService(t *testing.T) {
	cases := map[string]struct {
		body    string
		details map[string]interface{}
	}{
		"empty body fields":       {`{}`, map[string]interface{}{"email": "required", "password": "required"}},
		"blank email":             {`{"email":"   ","password":"password1"}`, map[string]interface{}{"email": "required"}},
		"invalid email":           {`{"email":"not-an-email","password":"password1"}`, map[string]interface{}{"email": "email"}},
		"email too long":          {`{"email":"` + strings.Repeat("a", 250) + `@example.com","password":"password1"}`, map[string]interface{}{"email": "max"}},
		"short password":          {`{"email":"ana@example.com","password":"1234567"}`, map[string]interface{}{"password": "length"}},
		"long password":           {`{"email":"ana@example.com","password":"` + strings.Repeat("p", 73) + `"}`, map[string]interface{}{"password": "length"}},
		"long multibyte password": {`{"email":"ana@example.com","password":"` + strings.Repeat("é", 37) + `"}`, map[string]interface{}{"password": "length"}},
		"everything invalid":      {`{"email":"x","password":"short"}`, map[string]interface{}{"email": "email", "password": "length"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			identities := &fakeIdentities{userID: uuid.New()}

			rec := callAuth(handlers.NewAuthHandler(identities, &fakeTokens{}).Register, tc.body)

			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := tests.FromJson(rec.Body.String())
			errorBody := body["error"].(map[string]interface{})
			assert.Equal(t, "VALIDATION_ERROR", errorBody["code"])
			assert.Equal(t, tc.details, errorBody["details"])
			assert.Empty(t, identities.calls)
		})
	}
}

func TestAuthHandlerRegisterAcceptsPasswordsAtTheByteLimits(t *testing.T) {
	for _, password := range []string{"12345678", strings.Repeat("p", 72), strings.Repeat("é", 36), "éééé"} {
		identities := &fakeIdentities{userID: uuid.New()}

		rec := callAuth(handlers.NewAuthHandler(identities, &fakeTokens{}).Register, `{"email":"ana@example.com","password":"`+password+`"}`)

		assert.Equal(t, http.StatusCreated, rec.Code, password)
	}
}

func TestAuthHandlersRejectBadBodies(t *testing.T) {
	handler := handlers.NewAuthHandler(&fakeIdentities{}, &fakeTokens{})
	large := `{"email":"ana@example.com","password":"` + strings.Repeat("p", 17<<10) + `"}`

	for name, fn := range map[string]http.HandlerFunc{"register": handler.Register, "login": handler.Login} {
		rec := callAuth(fn, `{"email":`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "INVALID_JSON", name)

		rec = callAuth(fn, `{"email":"ana@example.com","password":"password1","user_id":"x"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)

		rec = callAuth(fn, large)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "request body exceeds 16 KiB", name)
	}
}

func TestAuthHandlerRegisterPassesUpstreamErrorsThrough(t *testing.T) {
	identities := &fakeIdentities{err: apperrors.Conflict("EMAIL_TAKEN", "e-mail already registered")}
	tokens := &fakeTokens{}

	rec := callAuth(handlers.NewAuthHandler(identities, tokens).Register, `{"email":"ana@example.com","password":"password1"}`)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"EMAIL_TAKEN","message":"e-mail already registered"}}`, rec.Body.String())
	assert.Empty(t, tokens.issued)
}

func TestAuthHandlerRegisterFailsWhenTheTokenCannotBeIssued(t *testing.T) {
	rec := callAuth(handlers.NewAuthHandler(&fakeIdentities{userID: uuid.New()}, &fakeTokens{err: errors.New("boom")}).Register, `{"email":"ana@example.com","password":"password1"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "INTERNAL_ERROR")
	assert.NotContains(t, rec.Body.String(), "signed-token")
}

func TestAuthHandlerLoginReturnsAToken(t *testing.T) {
	userID := uuid.New()
	identities := &fakeIdentities{userID: userID}
	tokens := &fakeTokens{}

	rec := callAuth(handlers.NewAuthHandler(identities, tokens).Login, `{"email":" ana@example.com ","password":"any"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"access_token":"signed-token","token_type":"Bearer","expires_in":3600}}`, rec.Body.String())
	assert.Equal(t, []string{"verify"}, identities.calls)
	assert.Equal(t, []string{"ana@example.com"}, identities.emails)
	assert.Equal(t, []uuid.UUID{userID}, tokens.issued)
}

func TestAuthHandlerLoginRequiresBothFields(t *testing.T) {
	identities := &fakeIdentities{}

	rec := callAuth(handlers.NewAuthHandler(identities, &fakeTokens{}).Login, `{"email":" "}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"email": "required", "password": "required"}, errorBody["details"])
	assert.Empty(t, identities.calls)
}

func TestAuthHandlerLoginLeavesCredentialChecksToTheAccountService(t *testing.T) {
	identities := &fakeIdentities{err: apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid e-mail or password")}
	tokens := &fakeTokens{}

	rec := callAuth(handlers.NewAuthHandler(identities, tokens).Login, `{"email":"not-an-email","password":"x"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_CREDENTIALS")
	assert.Equal(t, []string{"verify"}, identities.calls)
	assert.Empty(t, tokens.issued)
}

func TestAuthHandlerLoginFailsWhenTheTokenCannotBeIssued(t *testing.T) {
	rec := callAuth(handlers.NewAuthHandler(&fakeIdentities{userID: uuid.New()}, &fakeTokens{err: errors.New("boom")}).Login, `{"email":"ana@example.com","password":"password1"}`)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "INTERNAL_ERROR")
}
