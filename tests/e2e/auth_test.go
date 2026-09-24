//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRequestsWithoutTokenAreRejected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method string
		path   string
		token  string
		body   interface{}
	}{
		{http.MethodGet, "/api/v1/accounts/" + uuid.NewString(), "", nil},
		{http.MethodGet, "/api/v1/users/" + uuid.NewString() + "/accounts", "", nil},
		{http.MethodGet, "/api/v1/transactions/" + uuid.NewString(), "", nil},
		{http.MethodPost, "/api/v1/transactions", "", movementBody(uuid.NewString(), "deposit", "10.00", key())},
		{http.MethodGet, "/api/v1/accounts/" + uuid.NewString(), "not-a-token", nil},
	}
	for _, c := range cases {
		status, header, raw := exchange(t, c.method, gateway()+c.path, c.token, c.body)
		require.Equal(t, http.StatusUnauthorized, status, "%s %s: %s", c.method, c.path, raw)
		require.Equal(t, "Bearer", header.Get("WWW-Authenticate"), "%s %s", c.method, c.path)
		out := decodeEnvelope(t, raw)
		require.False(t, out.Success)
		require.NotNil(t, out.Error, "%s %s: %s", c.method, c.path, raw)
		require.Equal(t, "UNAUTHORIZED", out.Error.Code)
	}
}

func TestLoginIssuesATokenOnlyForTheRightPassword(t *testing.T) {
	t.Parallel()
	user := newUser(t)

	status, data := login(t, user.email, user.password)
	require.Equal(t, http.StatusOK, status, "login: %v", data)
	require.Equal(t, "Bearer", data["token_type"])
	require.Positive(t, data["expires_in"])
	token, _ := data["access_token"].(string)
	require.NotEmpty(t, token)
	require.Empty(t, list(t, token, "/api/v1/users/"+user.userID+"/accounts"))

	status, rejection := postRejected(t, "", "/api/v1/auth/login", map[string]interface{}{"email": user.email, "password": user.password + "x"})
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "INVALID_CREDENTIALS", rejection.Code)

	status, rejection = postRejected(t, "", "/api/v1/auth/login", map[string]interface{}{"email": uniqueEmail(), "password": user.password})
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "INVALID_CREDENTIALS", rejection.Code)
}

func TestDuplicateRegistrationIsRejected(t *testing.T) {
	t.Parallel()
	user := newUser(t)

	status, rejection := postRejected(t, "", "/api/v1/auth/register", map[string]interface{}{"email": user.email, "password": strongPassword()})
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "EMAIL_TAKEN", rejection.Code)
}

func TestOtherUsersCannotReachAnAccount(t *testing.T) {
	t.Parallel()
	owner := newCustomer(t, "")
	other := newCustomer(t, "")

	deposit(t, owner, "100.00")
	depositID := transaction(t, owner, movement(t, owner, "deposit", "1.00"))["transaction_id"].(string)
	paymentKey := pay(t, owner, map[string]interface{}{"payment_method": "pix", "amount": "10.00", "pix_key": "ana@example.com"})
	paymentID := payment(t, owner, paymentKey)["payment_id"].(string)

	status, data := get(t, owner.token, "/api/v1/payments/"+paymentID)
	require.Equal(t, http.StatusOK, status, "owner reading payment: %v", data)
	status, data = get(t, owner.token, "/api/v1/transactions/"+depositID)
	require.Equal(t, http.StatusOK, status, "owner reading transaction: %v", data)

	reads := []string{
		"/api/v1/accounts/" + owner.accountID,
		"/api/v1/accounts/" + owner.accountID + "/transactions",
		"/api/v1/accounts/" + owner.accountID + "/payments",
		"/api/v1/transactions/" + depositID,
		"/api/v1/payments/" + paymentID,
		"/api/v1/users/" + owner.userID + "/notifications",
		"/api/v1/users/" + owner.userID + "/accounts",
	}
	for _, path := range reads {
		status, rejection := getRejected(t, other.token, path)
		require.Equal(t, http.StatusForbidden, status, "GET %s", path)
		require.Equal(t, "FORBIDDEN", rejection.Code, "GET %s", path)
	}

	commands := []struct {
		method string
		path   string
		body   interface{}
	}{
		{http.MethodPost, "/api/v1/transfers", transferBody(owner.accountID, other.accountID, "50.00", key())},
		{http.MethodPost, "/api/v1/transactions", movementBody(owner.accountID, "withdrawal", "50.00", key())},
		{http.MethodPost, "/api/v1/payments", map[string]interface{}{
			"account_id":      owner.accountID,
			"payment_method":  "pix",
			"amount":          "50.00",
			"currency":        "BRL",
			"recipient":       "Destinatário E2E",
			"pix_key":         "ana@example.com",
			"idempotency_key": key(),
		}},
		{http.MethodPatch, "/api/v1/accounts/" + owner.accountID, map[string]interface{}{"name": "Outro Cliente"}},
		{http.MethodPost, "/api/v1/accounts", map[string]interface{}{
			"user_id":      owner.userID,
			"account_type": "savings",
			"name":         "Cliente E2E",
			"email":        other.email,
			"document":     cpfs[0],
		}},
	}
	for _, c := range commands {
		status, rejection := rejected(t, c.method, other.token, c.path, c.body)
		require.Equal(t, http.StatusForbidden, status, "%s %s", c.method, c.path)
		require.Equal(t, "FORBIDDEN", rejection.Code, "%s %s", c.method, c.path)
	}

	for _, path := range []string{"/api/v1/accounts/" + uuid.NewString(), "/api/v1/accounts/" + uuid.NewString() + "/transactions"} {
		status, rejection := getRejected(t, other.token, path)
		require.Equal(t, http.StatusNotFound, status, "GET %s", path)
		require.Equal(t, "ACCOUNT_NOT_FOUND", rejection.Code, "GET %s", path)
	}

	require.Equal(t, "91.00", balance(t, owner))
	require.Equal(t, "0.00", balance(t, other))
	require.Len(t, list(t, owner.token, "/api/v1/users/"+owner.userID+"/accounts"), 1)
	require.Len(t, list(t, other.token, "/api/v1/users/"+other.userID+"/accounts"), 1)
}
