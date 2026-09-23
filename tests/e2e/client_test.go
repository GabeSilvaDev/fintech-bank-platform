//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	pollInterval   = 250 * time.Millisecond
	defaultTimeout = 30 * time.Second
	maxAttempts    = 10
)

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
	cpfs       = []string{"52998224725", "11144477735", "12345678909", "98765432100"}
	cpfCursor  atomic.Uint64

	transactionFinal = []string{"completed", "failed", "reversed", "reversal_failed"}
	paymentFinal     = []string{"completed", "failed", "refunded", "refund_failed"}
)

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	} `json:"error"`
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func gateway() string {
	return env("GATEWAY_URL", "http://localhost:8081")
}

func mailpit() string {
	return env("MAILPIT_URL", "http://localhost:8025")
}

func TestMain(m *testing.M) {
	resp, err := httpClient.Get(gateway() + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("skipping e2e suite: gateway at %s is unreachable\n", gateway())
		os.Exit(0)
	}
	resp.Body.Close()
	os.Exit(m.Run())
}

func retryAfter(header string) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second
}

func send(t *testing.T, method, url string, body interface{}) (int, []byte) {
	t.Helper()
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		payload = encoded
	}
	for attempt := 1; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequest(method, url, reader)
		require.NoError(t, err)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts {
			time.Sleep(retryAfter(resp.Header.Get("Retry-After")))
			continue
		}
		return resp.StatusCode, raw
	}
}

func decodeEnvelope(t *testing.T, raw []byte) envelope {
	t.Helper()
	var out envelope
	require.NoError(t, json.Unmarshal(raw, &out), "response is not an envelope: %s", raw)
	return out
}

func post(t *testing.T, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	status, raw := send(t, http.MethodPost, gateway()+path, body)
	out := decodeEnvelope(t, raw)
	var data map[string]interface{}
	if len(out.Data) > 0 {
		require.NoError(t, json.Unmarshal(out.Data, &data))
	}
	return status, data
}

func get(t *testing.T, path string) (int, interface{}) {
	t.Helper()
	status, raw := send(t, http.MethodGet, gateway()+path, nil)
	out := decodeEnvelope(t, raw)
	var data interface{}
	if len(out.Data) > 0 {
		require.NoError(t, json.Unmarshal(out.Data, &data))
	}
	return status, data
}

func list(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	status, data := get(t, path)
	require.Equal(t, http.StatusOK, status, "GET %s", path)
	raw, ok := data.([]interface{})
	require.True(t, ok, "GET %s did not return a list: %v", path, data)
	items := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		items = append(items, item.(map[string]interface{}))
	}
	return items
}

func accepted(t *testing.T, path string, body interface{}) {
	t.Helper()
	status, data := post(t, path, body)
	require.Equal(t, http.StatusAccepted, status, "POST %s", path)
	require.NotEmpty(t, data["command_id"])
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool, msgAndArgs ...interface{}) {
	t.Helper()
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			require.FailNow(t, fmt.Sprintf("condition not met within %s", timeout), msgAndArgs...)
		}
		time.Sleep(pollInterval)
	}
}

func key() string {
	return uuid.NewString()
}

func emailOf(userID string) string {
	return userID + "@e2e.test"
}

func newCustomer(t *testing.T, phone string) (string, string) {
	t.Helper()
	userID := uuid.NewString()
	body := map[string]interface{}{
		"user_id":      userID,
		"account_type": "checking",
		"name":         "Cliente E2E",
		"email":        emailOf(userID),
		"document":     cpfs[cpfCursor.Add(1)%uint64(len(cpfs))],
	}
	if phone != "" {
		body["phone"] = phone
	}
	accepted(t, "/api/v1/accounts", body)

	var accountID string
	eventually(t, 0, func() bool {
		accounts := list(t, "/api/v1/users/"+userID+"/accounts")
		if len(accounts) != 1 {
			return false
		}
		accountID = accounts[0]["account_id"].(string)
		return true
	}, "account for user %s was not created", userID)
	return userID, accountID
}

func balance(t *testing.T, accountID string) float64 {
	t.Helper()
	status, data := get(t, "/api/v1/accounts/"+accountID)
	require.Equal(t, http.StatusOK, status)
	return data.(map[string]interface{})["balance"].(float64)
}

func contains(values []string, value interface{}) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func settled(t *testing.T, timeout time.Duration, path, idempotencyKey string, final []string) map[string]interface{} {
	t.Helper()
	var last map[string]interface{}
	eventually(t, timeout, func() bool {
		for _, item := range list(t, path) {
			if item["idempotency_key"] == idempotencyKey {
				last = item
				return contains(final, item["status"])
			}
		}
		return false
	}, "%s with idempotency_key %s did not settle, last seen: %v", path, idempotencyKey, &last)
	return last
}

func transaction(t *testing.T, accountID, idempotencyKey string) map[string]interface{} {
	t.Helper()
	return settled(t, 0, "/api/v1/accounts/"+accountID+"/transactions", idempotencyKey, transactionFinal)
}

func payment(t *testing.T, accountID, idempotencyKey string) map[string]interface{} {
	t.Helper()
	return settled(t, 60*time.Second, "/api/v1/accounts/"+accountID+"/payments", idempotencyKey, paymentFinal)
}

func movement(t *testing.T, accountID, kind string, amount float64) string {
	t.Helper()
	k := key()
	accepted(t, "/api/v1/transactions", map[string]interface{}{
		"account_id":      accountID,
		"type":            kind,
		"amount":          amount,
		"currency":        "BRL",
		"description":     "e2e " + kind,
		"idempotency_key": k,
	})
	return k
}

func deposit(t *testing.T, accountID string, amount float64) {
	t.Helper()
	tx := transaction(t, accountID, movement(t, accountID, "deposit", amount))
	require.Equal(t, "completed", tx["status"], "deposit: %v", tx)
}

func transfer(t *testing.T, fromAccountID, toAccountID string, amount float64) string {
	t.Helper()
	k := key()
	accepted(t, "/api/v1/transfers", map[string]interface{}{
		"from_account_id": fromAccountID,
		"to_account_id":   toAccountID,
		"amount":          amount,
		"currency":        "BRL",
		"description":     "e2e transfer",
		"idempotency_key": k,
	})
	return k
}
