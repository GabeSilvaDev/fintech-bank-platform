//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func find(t *testing.T, accountID, idempotencyKey string) map[string]interface{} {
	t.Helper()
	for _, item := range list(t, "/api/v1/accounts/"+accountID+"/transactions") {
		if item["idempotency_key"] == idempotencyKey {
			return item
		}
	}
	require.FailNow(t, "transaction not listed", "account %s has no transaction %s", accountID, idempotencyKey)
	return nil
}

func byID(t *testing.T, transactionID string) map[string]interface{} {
	t.Helper()
	status, data := get(t, "/api/v1/transactions/"+transactionID)
	require.Equal(t, http.StatusOK, status, "GET transaction %s", transactionID)
	return data.(map[string]interface{})
}

func TestDepositWithdrawalAndTransfer(t *testing.T) {
	t.Parallel()
	_, sender := newCustomer(t, "")
	_, receiver := newCustomer(t, "")

	deposit(t, sender, 1000)
	require.InDelta(t, 1000, balance(t, sender), 0.001)

	withdrawalKey := movement(t, sender, "withdrawal", 150)
	withdrawal := transaction(t, sender, withdrawalKey)
	require.Equal(t, "completed", withdrawal["status"], "withdrawal: %v", withdrawal)
	require.InDelta(t, 850, balance(t, sender), 0.001)

	transferKey := transfer(t, sender, receiver, 300)
	sent := transaction(t, sender, transferKey)
	require.Equal(t, "completed", sent["status"], "transfer: %v", sent)
	require.Equal(t, "transfer", sent["type"])
	require.Equal(t, receiver, sent["counterparty_id"])
	require.InDelta(t, 550, sent["from_balance_after"].(float64), 0.001)
	require.NotContains(t, sent, "to_balance_after")

	received := find(t, receiver, transferKey)
	require.Equal(t, sent["transaction_id"], received["transaction_id"])
	require.Equal(t, "completed", received["status"])
	require.InDelta(t, 300, received["to_balance_after"].(float64), 0.001)
	require.NotContains(t, received, "from_balance_after")

	detail := byID(t, sent["transaction_id"].(string))
	require.Equal(t, "completed", detail["status"])
	require.Equal(t, receiver, detail["counterparty_id"])
	require.NotContains(t, detail, "from_balance_after")
	require.NotContains(t, detail, "to_balance_after")

	withdrawalDetail := byID(t, withdrawal["transaction_id"].(string))
	require.Equal(t, "completed", withdrawalDetail["status"])
	require.NotContains(t, withdrawalDetail, "from_balance_after")
	require.NotContains(t, withdrawalDetail, "to_balance_after")

	require.InDelta(t, 550, balance(t, sender), 0.001)
	require.InDelta(t, 300, balance(t, receiver), 0.001)

	overdraft := transaction(t, sender, movement(t, sender, "withdrawal", 10000))
	require.Equal(t, "failed", overdraft["status"], "overdraft: %v", overdraft)
	require.Equal(t, "insufficient_funds", overdraft["failure_reason"])
	require.InDelta(t, 550, balance(t, sender), 0.001)

	senderCount := len(list(t, "/api/v1/accounts/"+sender+"/transactions"))
	receiverCount := len(list(t, "/api/v1/accounts/"+receiver+"/transactions"))

	accepted(t, "/api/v1/transactions", map[string]interface{}{
		"account_id":      sender,
		"type":            "withdrawal",
		"amount":          150,
		"currency":        "BRL",
		"description":     "e2e withdrawal",
		"idempotency_key": withdrawalKey,
	})
	accepted(t, "/api/v1/transfers", map[string]interface{}{
		"from_account_id": sender,
		"to_account_id":   receiver,
		"amount":          300,
		"currency":        "BRL",
		"description":     "e2e transfer",
		"idempotency_key": transferKey,
	})
	deposit(t, sender, 1)

	require.Len(t, list(t, "/api/v1/accounts/"+sender+"/transactions"), senderCount+1)
	require.Len(t, list(t, "/api/v1/accounts/"+receiver+"/transactions"), receiverCount)
	require.InDelta(t, 551, balance(t, sender), 0.001)
	require.InDelta(t, 300, balance(t, receiver), 0.001)
	require.Equal(t, withdrawal["transaction_id"], find(t, sender, withdrawalKey)["transaction_id"])
	require.Equal(t, sent["transaction_id"], find(t, sender, transferKey)["transaction_id"])
}

func TestTransferToUnknownAccountIsReversed(t *testing.T) {
	t.Parallel()
	_, sender := newCustomer(t, "")
	deposit(t, sender, 500)

	unknown := uuid.NewString()
	tx := transaction(t, sender, transfer(t, sender, unknown, 120))
	require.Equal(t, "reversed", tx["status"], "transfer: %v", tx)
	require.Equal(t, "account_not_found", tx["failure_reason"])
	require.Equal(t, unknown, tx["counterparty_id"])
	require.InDelta(t, 500, balance(t, sender), 0.001)
}

func TestIdempotencyKeysArePerAccount(t *testing.T) {
	t.Parallel()
	_, first := newCustomer(t, "")
	_, second := newCustomer(t, "")

	shared := key()
	body := func(accountID string, amount float64) map[string]interface{} {
		return map[string]interface{}{
			"account_id":      accountID,
			"type":            "deposit",
			"amount":          amount,
			"currency":        "BRL",
			"description":     "e2e shared key",
			"idempotency_key": shared,
		}
	}

	accepted(t, "/api/v1/transactions", body(first, 10))
	accepted(t, "/api/v1/transactions", body(second, 20))

	firstTx := transaction(t, first, shared)
	secondTx := transaction(t, second, shared)
	require.Equal(t, "completed", firstTx["status"], "first: %v", firstTx)
	require.Equal(t, "completed", secondTx["status"], "second: %v", secondTx)
	require.NotEqual(t, firstTx["transaction_id"], secondTx["transaction_id"])
	require.Equal(t, first, firstTx["account_id"])
	require.Equal(t, second, secondTx["account_id"])
	require.InDelta(t, 10, balance(t, first), 0.001)
	require.InDelta(t, 20, balance(t, second), 0.001)

	accepted(t, "/api/v1/transactions", body(first, 10))
	deposit(t, first, 1)

	require.Len(t, list(t, "/api/v1/accounts/"+first+"/transactions"), 2)
	require.Len(t, list(t, "/api/v1/accounts/"+second+"/transactions"), 1)
	require.Equal(t, firstTx["transaction_id"], find(t, first, shared)["transaction_id"])
	require.InDelta(t, 11, balance(t, first), 0.001)
	require.InDelta(t, 20, balance(t, second), 0.001)
}

func TestIdempotencyKeyWithSpaceIsRejected(t *testing.T) {
	t.Parallel()
	status, rejection := postRejected(t, "/api/v1/transactions", map[string]interface{}{
		"account_id":      uuid.NewString(),
		"type":            "deposit",
		"amount":          10,
		"currency":        "BRL",
		"idempotency_key": "e2e key",
	})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "VALIDATION_ERROR", rejection.Code)
	require.Equal(t, map[string]string{"idempotency_key": "idempotency_key"}, rejection.Details)
}
