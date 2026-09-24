package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/auth"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func protectedHandler(seen *uuid.UUID) http.Handler {
	return auth.RequireAuth(auth.NewVerifier(tests.JWTSecret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := auth.UserID(r.Context())
		if ok {
			*seen = userID
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func serveWithAuthorization(handler http.Handler, authorization string, set bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/x", nil)
	if set {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestRequireAuthPassesTheUserIDToTheHandler(t *testing.T) {
	userID := uuid.New()
	var seen uuid.UUID

	rec := serveWithAuthorization(protectedHandler(&seen), tests.BearerToken(userID), true)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, userID, seen)
}

func TestRequireAuthAcceptsTheSchemeInAnyCase(t *testing.T) {
	userID := uuid.New()
	token := tests.AccessToken(userID)

	for _, scheme := range []string{"bearer", "BEARER", "BeArEr"} {
		var seen uuid.UUID
		rec := serveWithAuthorization(protectedHandler(&seen), scheme+" "+token, true)

		assert.Equal(t, http.StatusNoContent, rec.Code, scheme)
		assert.Equal(t, userID, seen, scheme)
	}
}

func TestRequireAuthRejectsMissingOrMalformedCredentials(t *testing.T) {
	token := tests.AccessToken(uuid.New())
	expired, _, err := auth.NewIssuer(tests.JWTSecret, -time.Hour).Issue(uuid.New())
	require.NoError(t, err)

	cases := map[string]string{
		"empty header":     "",
		"scheme only":      "Bearer",
		"scheme and space": "Bearer ",
		"basic scheme":     "Basic " + token,
		"token only":       token,
		"garbage token":    "Bearer abc.def.ghi",
		"expired token":    "Bearer " + expired,
		"other secret":     "Bearer " + issueWithSecret(t, "a-completely-different-secret-0123456789"),
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			seen := uuid.Nil
			rec := serveWithAuthorization(protectedHandler(&seen), header, true)

			assertUnauthorized(t, rec)
			assert.Equal(t, uuid.Nil, seen)
		})
	}

	t.Run("no header", func(t *testing.T) {
		seen := uuid.Nil
		assertUnauthorized(t, serveWithAuthorization(protectedHandler(&seen), "", false))
	})
}

func TestUserIDIsAbsentWithoutAuthentication(t *testing.T) {
	userID, ok := auth.UserID(context.Background())

	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, userID)
}

func TestWithUserIDStoresTheUserID(t *testing.T) {
	userID := uuid.New()

	got, ok := auth.UserID(auth.WithUserID(context.Background(), userID))

	assert.True(t, ok)
	assert.Equal(t, userID, got)
}

func issueWithSecret(t *testing.T, secret string) string {
	t.Helper()
	token, _, err := auth.NewIssuer(secret, time.Hour).Issue(uuid.New())
	require.NoError(t, err)
	return token
}

func assertUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"success":false,"error":{"code":"UNAUTHORIZED","message":"authentication required"}}`, rec.Body.String())
}
