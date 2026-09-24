package unit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
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

type fakeSessions struct {
	userID    uuid.UUID
	lifetime  time.Duration
	startErr  error
	rotateErr error
	revokeErr error
	started   []uuid.UUID
	rotated   []string
	revoked   []string
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{lifetime: 720*time.Hour + 500*time.Millisecond}
}

func (f *fakeSessions) StartSession(_ context.Context, userID uuid.UUID) (contracts.Session, error) {
	f.started = append(f.started, userID)
	if f.startErr != nil {
		return contracts.Session{}, f.startErr
	}
	return contracts.Session{RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(f.lifetime)}, nil
}

func (f *fakeSessions) RotateSession(_ context.Context, refreshToken string) (uuid.UUID, contracts.Session, error) {
	f.rotated = append(f.rotated, refreshToken)
	if f.rotateErr != nil {
		return uuid.Nil, contracts.Session{}, f.rotateErr
	}
	return f.userID, contracts.Session{RefreshToken: "refresh-2", ExpiresAt: time.Now().Add(f.lifetime)}, nil
}

func (f *fakeSessions) RevokeSession(_ context.Context, refreshToken string) error {
	f.revoked = append(f.revoked, refreshToken)
	return f.revokeErr
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

	rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), tokens).Register, `{"email":"  Ana@Example.com ","password":"password1"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"user_id":"`+userID.String()+`","access_token":"signed-token","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-1","refresh_expires_in":2592000}}`, rec.Body.String())
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

			rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), &fakeTokens{}).Register, tc.body)

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

		rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), &fakeTokens{}).Register, `{"email":"ana@example.com","password":"`+password+`"}`)

		assert.Equal(t, http.StatusCreated, rec.Code, password)
	}
}

func TestAuthHandlersRejectBadBodies(t *testing.T) {
	handler := handlers.NewAuthHandler(&fakeIdentities{}, newFakeSessions(), &fakeTokens{})
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

	rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), tokens).Register, `{"email":"ana@example.com","password":"password1"}`)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"EMAIL_TAKEN","message":"e-mail already registered"}}`, rec.Body.String())
	assert.Empty(t, rec.Header().Values("WWW-Authenticate"))
	assert.Empty(t, tokens.issued)
}

func TestAuthHandlersAnnounceTheBearerSchemeOnInvalidCredentials(t *testing.T) {
	identities := &fakeIdentities{err: apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid e-mail or password")}
	handler := handlers.NewAuthHandler(identities, newFakeSessions(), &fakeTokens{})

	for name, fn := range map[string]http.HandlerFunc{"register": handler.Register, "login": handler.Login} {
		rec := callAuth(fn, `{"email":"ana@example.com","password":"password1"}`)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
		assert.Equal(t, []string{"Bearer"}, rec.Header().Values("WWW-Authenticate"), name)
		assert.Contains(t, rec.Body.String(), "INVALID_CREDENTIALS", name)
	}
}

func TestAuthHandlersPassABusyAccountServiceThrough(t *testing.T) {
	identities := &fakeIdentities{err: apperrors.ServiceUnavailable("SERVICE_BUSY", "account service is busy, try again shortly")}
	handler := handlers.NewAuthHandler(identities, newFakeSessions(), &fakeTokens{})

	for name, fn := range map[string]http.HandlerFunc{"register": handler.Register, "login": handler.Login} {
		rec := callAuth(fn, `{"email":"ana@example.com","password":"password1"}`)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "SERVICE_BUSY", name)
		assert.Empty(t, rec.Header().Values("WWW-Authenticate"), name)
	}
}

func TestAuthHandlerRegisterFailsWhenTheTokenCannotBeIssued(t *testing.T) {
	rec := callAuth(handlers.NewAuthHandler(&fakeIdentities{userID: uuid.New()}, newFakeSessions(), &fakeTokens{err: errors.New("boom")}).Register, `{"email":"ana@example.com","password":"password1"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "INTERNAL_ERROR")
	assert.NotContains(t, rec.Body.String(), "signed-token")
}

func TestAuthHandlerLoginReturnsAToken(t *testing.T) {
	userID := uuid.New()
	identities := &fakeIdentities{userID: userID}
	tokens := &fakeTokens{}

	rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), tokens).Login, `{"email":" ana@example.com ","password":"any"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"access_token":"signed-token","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-1","refresh_expires_in":2592000}}`, rec.Body.String())
	assert.Equal(t, []string{"verify"}, identities.calls)
	assert.Equal(t, []string{"ana@example.com"}, identities.emails)
	assert.Equal(t, []uuid.UUID{userID}, tokens.issued)
}

func TestAuthHandlerLoginRequiresBothFields(t *testing.T) {
	identities := &fakeIdentities{}

	rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), &fakeTokens{}).Login, `{"email":" "}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"email": "required", "password": "required"}, errorBody["details"])
	assert.Empty(t, identities.calls)
}

func TestAuthHandlerLoginLeavesCredentialChecksToTheAccountService(t *testing.T) {
	identities := &fakeIdentities{err: apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid e-mail or password")}
	tokens := &fakeTokens{}

	rec := callAuth(handlers.NewAuthHandler(identities, newFakeSessions(), tokens).Login, `{"email":"not-an-email","password":"x"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_CREDENTIALS")
	assert.Equal(t, []string{"verify"}, identities.calls)
	assert.Empty(t, tokens.issued)
}

func TestAuthHandlerLoginFailsWhenTheTokenCannotBeIssued(t *testing.T) {
	rec := callAuth(handlers.NewAuthHandler(&fakeIdentities{userID: uuid.New()}, newFakeSessions(), &fakeTokens{err: errors.New("boom")}).Login, `{"email":"ana@example.com","password":"password1"}`)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "INTERNAL_ERROR")
}

func sessionHandler(identities *fakeIdentities, sessions *fakeSessions, tokens *fakeTokens) *handlers.AuthHandler {
	return handlers.NewAuthHandler(identities, sessions, tokens)
}

func TestAuthHandlerRegisterAndLoginStartASessionForTheUser(t *testing.T) {
	userID := uuid.New()
	sessions := newFakeSessions()
	tokens := &fakeTokens{}
	handler := sessionHandler(&fakeIdentities{userID: userID}, sessions, tokens)

	for name, fn := range map[string]http.HandlerFunc{"register": handler.Register, "login": handler.Login} {
		rec := callAuth(fn, `{"email":"ana@example.com","password":"password1"}`)

		assert.Contains(t, []int{http.StatusCreated, http.StatusOK}, rec.Code, name)
		data := tests.FromJson(rec.Body.String())["data"].(map[string]interface{})
		assert.Equal(t, "refresh-1", data["refresh_token"], name)
		assert.Equal(t, float64(2592000), data["refresh_expires_in"], name)
	}
	assert.Equal(t, []uuid.UUID{userID, userID}, sessions.started)
	assert.Equal(t, []uuid.UUID{userID, userID}, tokens.issued)
}

func TestAuthHandlerRegisterAndLoginFailWhenTheSessionCannotStart(t *testing.T) {
	cases := map[string]struct {
		err        error
		status     int
		code       string
		retryAfter string
	}{
		"account service down": {apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway), http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", ""},
		"account service busy": {apperrors.ServiceUnavailable("SERVICE_BUSY", "account service is busy, try again shortly").Wrap(contracts.RetryAfter("1")), http.StatusServiceUnavailable, "SERVICE_BUSY", "1"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sessions := newFakeSessions()
			sessions.startErr = tc.err
			tokens := &fakeTokens{}
			identities := &fakeIdentities{userID: uuid.New()}
			handler := sessionHandler(identities, sessions, tokens)

			for operation, fn := range map[string]http.HandlerFunc{"register": handler.Register, "login": handler.Login} {
				rec := callAuth(fn, `{"email":"ana@example.com","password":"password1"}`)

				assert.Equal(t, tc.status, rec.Code, operation)
				assert.Contains(t, rec.Body.String(), tc.code, operation)
				assert.Equal(t, tc.retryAfter, rec.Header().Get("Retry-After"), operation)
				assert.NotContains(t, rec.Body.String(), "access_token", operation)
			}
			assert.ElementsMatch(t, []string{"register", "verify"}, identities.calls)
			assert.Empty(t, tokens.issued)
		})
	}
}

func TestAuthHandlerReportsAnExpiredSessionAsZeroSecondsLeft(t *testing.T) {
	sessions := newFakeSessions()
	sessions.lifetime = -time.Minute

	rec := callAuth(sessionHandler(&fakeIdentities{userID: uuid.New()}, sessions, &fakeTokens{}).Login, `{"email":"ana@example.com","password":"password1"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(0), tests.FromJson(rec.Body.String())["data"].(map[string]interface{})["refresh_expires_in"])
}

func TestAuthHandlersForwardRetryAfterFromTheAccountService(t *testing.T) {
	cases := map[string]struct {
		err        error
		status     int
		retryAfter string
	}{
		"locked out":         {apperrors.TooManyRequests("TOO_MANY_ATTEMPTS", "too many failed attempts; try again later").Wrap(contracts.RetryAfter("840")), http.StatusTooManyRequests, "840"},
		"busy":               {apperrors.ServiceUnavailable("SERVICE_BUSY", "account service is busy, try again shortly").Wrap(contracts.RetryAfter("1")), http.StatusServiceUnavailable, "1"},
		"locked out no hint": {apperrors.TooManyRequests("TOO_MANY_ATTEMPTS", "too many failed attempts; try again later"), http.StatusTooManyRequests, ""},
		"other failure":      {apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway).Wrap(contracts.RetryAfter("9")), http.StatusBadGateway, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := callAuth(sessionHandler(&fakeIdentities{err: tc.err}, newFakeSessions(), &fakeTokens{}).Login, `{"email":"ana@example.com","password":"password1"}`)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.retryAfter, rec.Header().Get("Retry-After"))
			assert.Empty(t, rec.Header().Values("WWW-Authenticate"))
		})
	}
}

func TestAuthHandlerRefreshRotatesTheSessionAndIssuesAnAccessToken(t *testing.T) {
	userID := uuid.New()
	sessions := newFakeSessions()
	sessions.userID = userID
	tokens := &fakeTokens{}

	rec := callAuth(sessionHandler(&fakeIdentities{}, sessions, tokens).Refresh, `{"refresh_token":"refresh-1"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"access_token":"signed-token","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-2","refresh_expires_in":2592000}}`, rec.Body.String())
	assert.Equal(t, []string{"refresh-1"}, sessions.rotated)
	assert.Equal(t, []uuid.UUID{userID}, tokens.issued)
}

func TestAuthHandlerRefreshRejectsAnInvalidSession(t *testing.T) {
	sessions := newFakeSessions()
	sessions.rotateErr = apperrors.Unauthorized("INVALID_SESSION", "refresh token is invalid or expired")
	tokens := &fakeTokens{}

	rec := callAuth(sessionHandler(&fakeIdentities{}, sessions, tokens).Refresh, `{"refresh_token":"reused"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"INVALID_SESSION","message":"refresh token is invalid or expired"}}`, rec.Body.String())
	assert.Equal(t, []string{"Bearer"}, rec.Header().Values("WWW-Authenticate"))
	assert.Empty(t, tokens.issued)
}

func TestAuthHandlerRefreshPassesUpstreamFailuresThrough(t *testing.T) {
	sessions := newFakeSessions()
	sessions.rotateErr = apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway)

	rec := callAuth(sessionHandler(&fakeIdentities{}, sessions, &fakeTokens{}).Refresh, `{"refresh_token":"refresh-1"}`)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "UPSTREAM_UNAVAILABLE")
	assert.Empty(t, rec.Header().Values("WWW-Authenticate"))
}

func TestAuthHandlerRefreshFailsWhenTheTokenCannotBeIssued(t *testing.T) {
	rec := callAuth(sessionHandler(&fakeIdentities{}, newFakeSessions(), &fakeTokens{err: errors.New("boom")}).Refresh, `{"refresh_token":"refresh-1"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "refresh-2")
}

func TestAuthHandlerLogoutRevokesTheSession(t *testing.T) {
	sessions := newFakeSessions()

	rec := callAuth(sessionHandler(&fakeIdentities{}, sessions, &fakeTokens{}).Logout, `{"refresh_token":"refresh-1"}`)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, []string{"refresh-1"}, sessions.revoked)
}

func TestAuthHandlerLogoutReportsAnUnreachableAccountService(t *testing.T) {
	sessions := newFakeSessions()
	sessions.revokeErr = apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway)

	rec := callAuth(sessionHandler(&fakeIdentities{}, sessions, &fakeTokens{}).Logout, `{"refresh_token":"refresh-1"}`)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "UPSTREAM_UNAVAILABLE")
}

func TestSessionHandlersRequireARefreshToken(t *testing.T) {
	sessions := newFakeSessions()
	handler := sessionHandler(&fakeIdentities{}, sessions, &fakeTokens{})

	for name, fn := range map[string]http.HandlerFunc{"refresh": handler.Refresh, "logout": handler.Logout} {
		for _, body := range []string{`{}`, `{"refresh_token":""}`} {
			rec := callAuth(fn, body)

			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
			errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
			assert.Equal(t, "VALIDATION_ERROR", errorBody["code"], name)
			assert.Equal(t, map[string]interface{}{"refresh_token": "required"}, errorBody["details"], name)
		}
	}
	assert.Empty(t, sessions.rotated)
	assert.Empty(t, sessions.revoked)
}

func TestSessionHandlersRejectBadBodies(t *testing.T) {
	sessions := newFakeSessions()
	handler := sessionHandler(&fakeIdentities{}, sessions, &fakeTokens{})
	large := `{"refresh_token":"` + strings.Repeat("r", 17<<10) + `"}`

	for name, fn := range map[string]http.HandlerFunc{"refresh": handler.Refresh, "logout": handler.Logout} {
		rec := callAuth(fn, `{"refresh_token":`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "INVALID_JSON", name)

		rec = callAuth(fn, `{"refresh_token":"refresh-1","user_id":"x"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)

		rec = callAuth(fn, large)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "request body exceeds 16 KiB", name)
	}
	assert.Empty(t, sessions.rotated)
	assert.Empty(t, sessions.revoked)
}

func TestSessionHandlersTurnAwayOversizedRefreshTokens(t *testing.T) {
	sessions := newFakeSessions()
	tokens := &fakeTokens{}
	handler := sessionHandler(&fakeIdentities{}, sessions, tokens)
	oversized := `{"refresh_token":"` + strings.Repeat("r", 257) + `"}`

	rec := callAuth(handler.Refresh, oversized)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"INVALID_SESSION","message":"refresh token is invalid or expired"}}`, rec.Body.String())
	assert.Equal(t, []string{"Bearer"}, rec.Header().Values("WWW-Authenticate"))

	rec = callAuth(handler.Logout, oversized)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	assert.Empty(t, sessions.rotated)
	assert.Empty(t, sessions.revoked)
	assert.Empty(t, tokens.issued)

	longest := strings.Repeat("r", 256)
	callAuth(handler.Refresh, `{"refresh_token":"`+longest+`"}`)
	callAuth(handler.Logout, `{"refresh_token":"`+longest+`"}`)
	assert.Equal(t, []string{longest}, sessions.rotated)
	assert.Equal(t, []string{longest}, sessions.revoked)
}
