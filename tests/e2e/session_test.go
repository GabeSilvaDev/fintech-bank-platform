//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	accessTokenSeconds = 900
	loginMaxFailures   = 5
	lockoutSeconds     = 900
)

type tokenPair struct {
	access  string
	refresh string
}

func pairFrom(t *testing.T, data map[string]interface{}) tokenPair {
	t.Helper()
	require.Equal(t, "Bearer", data["token_type"], "token response: %v", data)
	require.EqualValues(t, accessTokenSeconds, data["expires_in"], "token response: %v", data)
	require.Positive(t, data["refresh_expires_in"], "token response: %v", data)
	pair := tokenPair{}
	pair.access, _ = data["access_token"].(string)
	pair.refresh, _ = data["refresh_token"].(string)
	require.NotEmpty(t, pair.access, "token response has no access_token: %v", data)
	require.NotEmpty(t, pair.refresh, "token response has no refresh_token: %v", data)
	return pair
}

func signIn(t *testing.T, user customer) tokenPair {
	t.Helper()
	status, data := login(t, user.email, user.password)
	require.Equal(t, http.StatusOK, status, "login %s: %v", user.email, data)
	return pairFrom(t, data)
}

func refresh(t *testing.T, refreshToken string) tokenPair {
	t.Helper()
	status, data := post(t, "", "/api/v1/auth/refresh", map[string]interface{}{"refresh_token": refreshToken})
	require.Equal(t, http.StatusOK, status, "refresh: %v", data)
	return pairFrom(t, data)
}

func refreshRejected(t *testing.T, refreshToken string) {
	t.Helper()
	status, header, raw := exchange(t, http.MethodPost, gateway()+"/api/v1/auth/refresh", "", map[string]interface{}{"refresh_token": refreshToken})
	require.Equal(t, http.StatusUnauthorized, status, "refresh: %s", raw)
	require.Equal(t, "Bearer", header.Get("WWW-Authenticate"))
	out := decodeEnvelope(t, raw)
	require.NotNil(t, out.Error, "refresh: %s", raw)
	require.Equal(t, "INVALID_SESSION", out.Error.Code)
}

func logout(t *testing.T, refreshToken string) {
	t.Helper()
	status, raw := send(t, http.MethodPost, gateway()+"/api/v1/auth/logout", "", map[string]interface{}{"refresh_token": refreshToken})
	require.Equal(t, http.StatusNoContent, status, "logout: %s", raw)
	require.Empty(t, raw)
}

func loginOnce(t *testing.T, email, password string) (int, http.Header, apiError) {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{"email": email, "password": password})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, gateway()+"/api/v1/auth/login", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	var rejection apiError
	if out := decodeEnvelope(t, raw); out.Error != nil {
		rejection = *out.Error
	}
	return resp.StatusCode, resp.Header, rejection
}

func failLogins(t *testing.T, email, password string, attempts int) {
	t.Helper()
	for attempt := 1; attempt <= attempts; attempt++ {
		status, _, rejection := loginOnce(t, email, password)
		require.Equal(t, http.StatusUnauthorized, status, "attempt %d: %v", attempt, rejection)
		require.Equal(t, "INVALID_CREDENTIALS", rejection.Code, "attempt %d", attempt)
	}
}

func requireLockedOut(t *testing.T, email, password string) {
	t.Helper()
	status, header, rejection := loginOnce(t, email, password)
	require.Equal(t, http.StatusTooManyRequests, status, "login %s: %v", email, rejection)
	require.Equal(t, "TOO_MANY_ATTEMPTS", rejection.Code)
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	require.NoError(t, err, "Retry-After %q", header.Get("Retry-After"))
	require.Positive(t, seconds)
	require.LessOrEqual(t, seconds, lockoutSeconds)
}

func TestRegisterAndLoginReturnAFifteenMinuteTokenPair(t *testing.T) {
	t.Parallel()
	email, password := uniqueEmail(), strongPassword()

	status, data := register(t, email, password)
	require.Equal(t, http.StatusCreated, status, "register: %v", data)
	registered := pairFrom(t, data)

	status, data = login(t, email, password)
	require.Equal(t, http.StatusOK, status, "login: %v", data)
	loggedIn := pairFrom(t, data)
	require.NotEqual(t, registered.refresh, loggedIn.refresh)

	refresh(t, registered.refresh)
	refresh(t, loggedIn.refresh)
}

func TestRefreshRotatesTheTokenPair(t *testing.T) {
	t.Parallel()
	user := newUser(t)
	first := signIn(t, user)

	second := refresh(t, first.refresh)
	require.NotEqual(t, first.access, second.access)
	require.NotEqual(t, first.refresh, second.refresh)
	require.Empty(t, list(t, second.access, "/api/v1/users/"+user.userID+"/accounts"))

	third := refresh(t, second.refresh)
	require.NotEqual(t, second.refresh, third.refresh)
	require.Empty(t, list(t, third.access, "/api/v1/users/"+user.userID+"/accounts"))

	refreshRejected(t, first.refresh)
}

func TestReusingARotatedRefreshTokenRevokesTheFamily(t *testing.T) {
	t.Parallel()
	user := newUser(t)
	first := signIn(t, user)
	second := refresh(t, first.refresh)

	refreshRejected(t, first.refresh)
	refreshRejected(t, second.refresh)

	other := signIn(t, user)
	refresh(t, other.refresh)
}

func TestLogoutRevokesTheSession(t *testing.T) {
	t.Parallel()
	user := newUser(t)
	first := signIn(t, user)
	second := refresh(t, first.refresh)
	untouched := signIn(t, user)

	logout(t, second.refresh)
	refreshRejected(t, second.refresh)
	refreshRejected(t, first.refresh)

	logout(t, second.refresh)
	logout(t, "not-a-refresh-token")

	refresh(t, untouched.refresh)
}

func TestLogoutAndRefreshRequireARefreshToken(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/auth/logout"} {
		status, rejection := postRejected(t, "", path, map[string]interface{}{})
		require.Equal(t, http.StatusUnprocessableEntity, status, "POST %s", path)
		require.Equal(t, "VALIDATION_ERROR", rejection.Code, "POST %s", path)
		require.Equal(t, "required", rejection.Details["refresh_token"], "POST %s", path)
	}
}

func TestRepeatedWrongPasswordsLockTheEmail(t *testing.T) {
	t.Parallel()
	user := newUser(t)
	bystander := newUser(t)

	failLogins(t, user.email, user.password+"x", loginMaxFailures)

	requireLockedOut(t, user.email, user.password)
	requireLockedOut(t, strings.ToUpper(user.email), user.password)
	signIn(t, bystander)
}

func TestUnknownEmailsLockLikeKnownOnes(t *testing.T) {
	t.Parallel()
	email := uniqueEmail()

	failLogins(t, email, strongPassword(), loginMaxFailures)

	requireLockedOut(t, email, strongPassword())
}

func TestSuccessfulLoginResetsTheFailureCount(t *testing.T) {
	t.Parallel()
	user := newUser(t)

	failLogins(t, user.email, user.password+"x", loginMaxFailures-1)
	signIn(t, user)
	failLogins(t, user.email, user.password+"x", loginMaxFailures-1)

	signIn(t, user)
}
