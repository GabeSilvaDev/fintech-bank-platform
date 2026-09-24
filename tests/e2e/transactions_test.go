//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func find(t *testing.T, c customer, idempotencyKey string) map[string]interface{} {
	t.Helper()
	for _, item := range list(t, c.token, "/api/v1/accounts/"+c.accountID+"/transactions") {
		if item["idempotency_key"] == idempotencyKey {
			return item
		}
	}
	require.FailNow(t, "transaction not listed", "account %s has no transaction %s", c.accountID, idempotencyKey)
	return nil
}

func byID(t *testing.T, c customer, transactionID string) map[string]interface{} {
	t.Helper()
	status, data := get(t, c.token, "/api/v1/transactions/"+transactionID)
	require.Equal(t, http.StatusOK, status, "GET transaction %s", transactionID)
	return data.(map[string]interface{})
}

func TestDepositWithdrawalAndTransfer(t *testing.T) {
	t.Parallel()
	sender := newCustomer(t, "")
	receiver := newCustomer(t, "")

	deposit(t, sender, "1000.00")
	require.Equal(t, "1000.00", balance(t, sender))

	withdrawalKey := movement(t, sender, "withdrawal", "150.00")
	withdrawal := transaction(t, sender, withdrawalKey)
	require.Equal(t, "completed", withdrawal["status"], "withdrawal: %v", withdrawal)
	require.Equal(t, "150.00", withdrawal["amount"])
	require.Equal(t, "850.00", balance(t, sender))

	transferKey := transfer(t, sender, receiver.accountID, "300.00")
	sent := transaction(t, sender, transferKey)
	require.Equal(t, "completed", sent["status"], "transfer: %v", sent)
	require.Equal(t, "transfer", sent["type"])
	require.Equal(t, receiver.accountID, sent["counterparty_id"])
	require.Equal(t, "300.00", sent["amount"])
	require.Equal(t, "550.00", sent["from_balance_after"])
	require.NotContains(t, sent, "to_balance_after")

	received := find(t, receiver, transferKey)
	require.Equal(t, sent["transaction_id"], received["transaction_id"])
	require.Equal(t, "completed", received["status"])
	require.Equal(t, "300.00", received["to_balance_after"])
	require.NotContains(t, received, "from_balance_after")

	detail := byID(t, sender, sent["transaction_id"].(string))
	require.Equal(t, "completed", detail["status"])
	require.Equal(t, "300.00", detail["amount"])
	require.Equal(t, receiver.accountID, detail["counterparty_id"])
	require.Equal(t, transferKey, detail["idempotency_key"])
	require.Equal(t, "e2e transfer", detail["description"])
	require.NotContains(t, detail, "from_balance_after")
	require.NotContains(t, detail, "to_balance_after")

	receiverDetail := byID(t, receiver, sent["transaction_id"].(string))
	require.Equal(t, sent["transaction_id"], receiverDetail["transaction_id"])
	require.Equal(t, sender.accountID, receiverDetail["account_id"])
	require.Equal(t, receiver.accountID, receiverDetail["counterparty_id"])
	require.NotContains(t, receiverDetail, "idempotency_key")
	require.NotContains(t, receiverDetail, "description")

	withdrawalDetail := byID(t, sender, withdrawal["transaction_id"].(string))
	require.Equal(t, "completed", withdrawalDetail["status"])
	require.NotContains(t, withdrawalDetail, "from_balance_after")
	require.NotContains(t, withdrawalDetail, "to_balance_after")

	require.Equal(t, "550.00", balance(t, sender))
	require.Equal(t, "300.00", balance(t, receiver))

	overdraft := transaction(t, sender, movement(t, sender, "withdrawal", "10000.00"))
	require.Equal(t, "failed", overdraft["status"], "overdraft: %v", overdraft)
	require.Equal(t, "insufficient_funds", overdraft["failure_reason"])
	require.Equal(t, "550.00", balance(t, sender))

	senderCount := len(list(t, sender.token, "/api/v1/accounts/"+sender.accountID+"/transactions"))
	receiverCount := len(list(t, receiver.token, "/api/v1/accounts/"+receiver.accountID+"/transactions"))

	accepted(t, sender.token, "/api/v1/transactions", movementBody(sender.accountID, "withdrawal", "150.00", withdrawalKey))
	accepted(t, sender.token, "/api/v1/transfers", transferBody(sender.accountID, receiver.accountID, "300.00", transferKey))
	deposit(t, sender, "1.00")

	require.Len(t, list(t, sender.token, "/api/v1/accounts/"+sender.accountID+"/transactions"), senderCount+1)
	require.Len(t, list(t, receiver.token, "/api/v1/accounts/"+receiver.accountID+"/transactions"), receiverCount)
	require.Equal(t, "551.00", balance(t, sender))
	require.Equal(t, "300.00", balance(t, receiver))
	require.Equal(t, withdrawal["transaction_id"], find(t, sender, withdrawalKey)["transaction_id"])
	require.Equal(t, sent["transaction_id"], find(t, sender, transferKey)["transaction_id"])
}

func TestTransferToUnknownAccountIsReversed(t *testing.T) {
	t.Parallel()
	sender := newCustomer(t, "")
	deposit(t, sender, "500.00")

	unknown := uuid.NewString()
	tx := transaction(t, sender, transfer(t, sender, unknown, "120.00"))
	require.Equal(t, "reversed", tx["status"], "transfer: %v", tx)
	require.Equal(t, "account_not_found", tx["failure_reason"])
	require.Equal(t, unknown, tx["counterparty_id"])
	require.Equal(t, "500.00", balance(t, sender))
}

func TestIdempotencyKeysArePerAccount(t *testing.T) {
	t.Parallel()
	first := newCustomer(t, "")
	second := newCustomer(t, "")

	shared := key()
	body := func(c customer, amount string) map[string]interface{} {
		return map[string]interface{}{
			"account_id":      c.accountID,
			"type":            "deposit",
			"amount":          amount,
			"currency":        "BRL",
			"description":     "e2e shared key",
			"idempotency_key": shared,
		}
	}

	accepted(t, first.token, "/api/v1/transactions", body(first, "10.00"))
	accepted(t, second.token, "/api/v1/transactions", body(second, "20.00"))

	firstTx := transaction(t, first, shared)
	secondTx := transaction(t, second, shared)
	require.Equal(t, "completed", firstTx["status"], "first: %v", firstTx)
	require.Equal(t, "completed", secondTx["status"], "second: %v", secondTx)
	require.NotEqual(t, firstTx["transaction_id"], secondTx["transaction_id"])
	require.Equal(t, first.accountID, firstTx["account_id"])
	require.Equal(t, second.accountID, secondTx["account_id"])
	require.Equal(t, "10.00", balance(t, first))
	require.Equal(t, "20.00", balance(t, second))

	accepted(t, first.token, "/api/v1/transactions", body(first, "10.00"))
	deposit(t, first, "1.00")

	require.Len(t, list(t, first.token, "/api/v1/accounts/"+first.accountID+"/transactions"), 2)
	require.Len(t, list(t, second.token, "/api/v1/accounts/"+second.accountID+"/transactions"), 1)
	require.Equal(t, firstTx["transaction_id"], find(t, first, shared)["transaction_id"])
	require.Equal(t, "11.00", balance(t, first))
	require.Equal(t, "20.00", balance(t, second))
}

func TestIdempotencyKeyWithSpaceIsRejected(t *testing.T) {
	t.Parallel()
	c := newUser(t)
	status, rejection := postRejected(t, c.token, "/api/v1/transactions", map[string]interface{}{
		"account_id":      uuid.NewString(),
		"type":            "deposit",
		"amount":          "10.00",
		"currency":        "BRL",
		"idempotency_key": "e2e key",
	})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "VALIDATION_ERROR", rejection.Code)
	require.Equal(t, map[string]string{"idempotency_key": "idempotency_key"}, rejection.Details)
}

func TestCentsAddUpExactly(t *testing.T) {
	t.Parallel()
	c := newCustomer(t, "")

	deposit(t, c, "0.10")
	deposit(t, c, "0.10")
	deposit(t, c, "0.10")
	require.Equal(t, "0.30", balance(t, c))

	withdrawal := transaction(t, c, movement(t, c, "withdrawal", "0.30"))
	require.Equal(t, "completed", withdrawal["status"], "withdrawal: %v", withdrawal)
	require.Equal(t, "0.30", withdrawal["amount"])
	require.Equal(t, "0.00", balance(t, c))
}

func TestLargeBalancesStayExact(t *testing.T) {
	t.Parallel()
	c := newCustomer(t, "")

	keys := make([]string, 0, 11)
	for i := 0; i < 10; i++ {
		keys = append(keys, movement(t, c, "deposit", "9999999999999.99"))
	}
	keys = append(keys, movement(t, c, "deposit", "0.09"))

	for _, k := range keys {
		tx := transaction(t, c, k)
		require.Equal(t, "completed", tx["status"], "deposit: %v", tx)
	}
	require.Equal(t, "99999999999999.99", balance(t, c))
}

func TestNumericAmountsAreStillAccepted(t *testing.T) {
	t.Parallel()
	c := newCustomer(t, "")

	k := key()
	accepted(t, c.token, "/api/v1/transactions", map[string]interface{}{
		"account_id":      c.accountID,
		"type":            "deposit",
		"amount":          1234.56,
		"currency":        "BRL",
		"description":     "e2e numeric deposit",
		"idempotency_key": k,
	})
	tx := transaction(t, c, k)
	require.Equal(t, "completed", tx["status"], "deposit: %v", tx)
	require.Equal(t, "1234.56", tx["amount"])
	require.Equal(t, "1234.56", balance(t, c))
}

func TestInvalidAmountsAreRejected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		amount interface{}
		detail string
	}{
		{"1.234", "amount"},
		{1.234, "amount"},
		{"1,00", "amount"},
		{"0.00", "gt"},
		{"-5.00", "gt"},
		{"10000000000000.00", "lte"},
	}
	user := newUser(t)
	for _, c := range cases {
		status, rejection := postRejected(t, user.token, "/api/v1/transactions", map[string]interface{}{
			"account_id":      uuid.NewString(),
			"type":            "deposit",
			"amount":          c.amount,
			"currency":        "BRL",
			"idempotency_key": key(),
		})
		require.Equal(t, http.StatusUnprocessableEntity, status, "amount %v", c.amount)
		require.Equal(t, "VALIDATION_ERROR", rejection.Code, "amount %v", c.amount)
		require.Equal(t, map[string]string{"amount": c.detail}, rejection.Details, "amount %v", c.amount)
	}
}
