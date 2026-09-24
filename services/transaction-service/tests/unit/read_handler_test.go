package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/transaction-service/internal/app/handlers"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func readRouter(h *harness) http.Handler {
	reads := handlers.NewReadHandler(h.service)
	r := chi.NewRouter()
	r.Get("/transactions/{id}", reads.GetTransaction)
	r.Get("/accounts/{account_id}/transactions", reads.ListAccountTransactions)
	return r
}

func get(handler http.Handler, path string) (*httptest.ResponseRecorder, map[string]interface{}) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, tests.FromJson(rec.Body.String())
}

func TestGetTransaction(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)
	tx.Description = "rent"
	h.repo.Put(tx)

	rec, body := get(readRouter(h), "/transactions/"+tx.ID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, tx.ID.String(), data["transaction_id"])
	assert.Equal(t, "transfer", data["type"])
	assert.Equal(t, "debited", data["status"])
	assert.Equal(t, tx.AccountID.String(), data["account_id"])
	assert.Equal(t, tx.CounterpartyID.String(), data["counterparty_id"])
	assert.Equal(t, "30.00", data["amount"])
	assert.Equal(t, "BRL", data["currency"])
	assert.Equal(t, "rent", data["description"])
	assert.NotContains(t, data, "from_balance_after")
	assert.NotContains(t, data, "to_balance_after")
	assert.NotContains(t, data, "failure_reason")
	assert.NotContains(t, data, "completed_at")

	rec, body = get(readRouter(h), "/transactions/not-a-uuid")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", body["error"].(map[string]interface{})["code"])

	rec, body = get(readRouter(h), "/transactions/"+uuid.NewString())
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "TRANSACTION_NOT_FOUND", body["error"].(map[string]interface{})["code"])

	h.repo.Err = errors.New("db down")
	rec, _ = get(readRouter(h), "/transactions/"+tx.ID.String())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestListAccountTransactions(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusCompleted)

	rec, body := get(readRouter(h), "/accounts/"+tx.AccountID.String()+"/transactions")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 1)

	rec, body = get(readRouter(h), "/accounts/"+uuid.NewString()+"/transactions?limit=5")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 0)

	for _, bad := range []string{"0", "201", "x", "-1"} {
		rec, body = get(readRouter(h), "/accounts/"+tx.AccountID.String()+"/transactions?limit="+bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, bad)
		assert.Equal(t, "range", body["error"].(map[string]interface{})["details"].(map[string]interface{})["limit"], bad)
	}

	rec, _ = get(readRouter(h), "/accounts/x/transactions")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	h.repo.Err = errors.New("db down")
	rec, _ = get(readRouter(h), "/accounts/"+tx.AccountID.String()+"/transactions")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestListAccountTransactionsPagesWithBeforeCursor(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	base := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	txs := make([]*models.Transaction, 3)
	for i := 0; i < 3; i++ {
		tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusCompleted, AccountID: account, AmountCents: int64(1000 + i), Currency: "BRL", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute)}
		h.repo.Put(tx)
		txs[i] = tx
	}

	rec, body := get(readRouter(h), "/accounts/"+account.String()+"/transactions?limit=2")
	assert.Equal(t, http.StatusOK, rec.Code)
	items := body["data"].([]interface{})
	assert.Len(t, items, 2)
	assert.Equal(t, txs[2].ID.String(), items[0].(map[string]interface{})["transaction_id"])
	assert.Equal(t, txs[1].ID.String(), items[1].(map[string]interface{})["transaction_id"])
	nextBefore := rec.Header().Get("X-Next-Before")
	assert.Equal(t, txs[1].CreatedAt.UTC().Format(time.RFC3339Nano), nextBefore)

	rec, body = get(readRouter(h), "/accounts/"+account.String()+"/transactions?limit=2&before="+nextBefore)
	assert.Equal(t, http.StatusOK, rec.Code)
	items = body["data"].([]interface{})
	assert.Len(t, items, 1)
	assert.Equal(t, txs[0].ID.String(), items[0].(map[string]interface{})["transaction_id"])
	assert.Empty(t, rec.Header().Get("X-Next-Before"))

	rec, body = get(readRouter(h), "/accounts/"+account.String()+"/transactions?before=not-a-date")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "datetime", body["error"].(map[string]interface{})["details"].(map[string]interface{})["before"])

	rec, _ = get(readRouter(h), "/accounts/"+account.String()+"/transactions?before=2026-09-20T10%3A00%3A00.123456789Z")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListAccountTransactionsReturnsNextBeforeWhenARecordIsNotYetVisible(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	base := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	txs := make([]*models.Transaction, 2)
	for i := 0; i < 2; i++ {
		tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusCompleted, AccountID: account, AmountCents: int64(1000 + i), Currency: "BRL", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute)}
		h.repo.Put(tx)
		txs[i] = tx
	}
	h.repo.Invisible = map[uuid.UUID]bool{txs[0].ID: true}

	rec, body := get(readRouter(h), "/accounts/"+account.String()+"/transactions?limit=2")

	assert.Equal(t, http.StatusOK, rec.Code)
	items := body["data"].([]interface{})
	assert.Len(t, items, 1)
	assert.Equal(t, txs[1].ID.String(), items[0].(map[string]interface{})["transaction_id"])
	assert.Equal(t, txs[0].CreatedAt.UTC().Format(time.RFC3339Nano), rec.Header().Get("X-Next-Before"))
}

func TestListAccountTransactionsDepositAndWithdrawalKeepOwnBalance(t *testing.T) {
	h := newHarness()
	withdrawal := h.pending(models.TypeWithdrawal, models.StatusDebited)
	deposit := h.pending(models.TypeDeposit, models.StatusCompleted)
	toBalance := int64(5000)
	deposit.ToBalanceCents = &toBalance
	h.repo.Put(deposit)

	rec, body := get(readRouter(h), "/accounts/"+withdrawal.AccountID.String()+"/transactions")
	assert.Equal(t, http.StatusOK, rec.Code)
	items := body["data"].([]interface{})
	assert.Len(t, items, 1)
	withdrawalView := items[0].(map[string]interface{})
	assert.Equal(t, "70.00", withdrawalView["from_balance_after"])
	assert.NotContains(t, withdrawalView, "to_balance_after")

	rec, body = get(readRouter(h), "/accounts/"+deposit.AccountID.String()+"/transactions")
	assert.Equal(t, http.StatusOK, rec.Code)
	items = body["data"].([]interface{})
	assert.Len(t, items, 1)
	depositView := items[0].(map[string]interface{})
	assert.Equal(t, "50.00", depositView["to_balance_after"])
	assert.NotContains(t, depositView, "from_balance_after")
}

func TestListAccountTransactionsTransferBalanceVisibility(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusCompleted)
	from := int64(7000)
	to := int64(3000)
	tx.FromBalanceCents = &from
	tx.ToBalanceCents = &to
	h.repo.Put(tx)

	rec, body := get(readRouter(h), "/accounts/"+tx.AccountID.String()+"/transactions")
	assert.Equal(t, http.StatusOK, rec.Code)
	items := body["data"].([]interface{})
	assert.Len(t, items, 1)
	senderView := items[0].(map[string]interface{})
	assert.Equal(t, "70.00", senderView["from_balance_after"])
	assert.NotContains(t, senderView, "to_balance_after")

	rec, body = get(readRouter(h), "/accounts/"+tx.CounterpartyID.String()+"/transactions")
	assert.Equal(t, http.StatusOK, rec.Code)
	items = body["data"].([]interface{})
	assert.Len(t, items, 1)
	counterpartyView := items[0].(map[string]interface{})
	assert.Equal(t, "30.00", counterpartyView["to_balance_after"])
	assert.NotContains(t, counterpartyView, "from_balance_after")
}

func TestGetTransactionReportsReversing(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusReversing)
	tx.FailureReason = "account_not_active"
	h.repo.Put(tx)

	rec, body := get(readRouter(h), "/transactions/"+tx.ID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, "transfer", data["type"])
	assert.Equal(t, "reversing", data["status"])
	assert.Equal(t, "account_not_active", data["failure_reason"])
	assert.Equal(t, tx.CounterpartyID.String(), data["counterparty_id"])
	assert.NotContains(t, data, "from_balance_after")
	assert.NotContains(t, data, "to_balance_after")
	assert.NotContains(t, data, "completed_at")
}

func TestGetTransactionOmitsBothBalancesEvenWhenBothAreSet(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusCompleted)
	toBalance := int64(9000)
	tx.ToBalanceCents = &toBalance
	h.repo.Put(tx)

	rec, body := get(readRouter(h), "/transactions/"+tx.ID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.NotContains(t, data, "from_balance_after")
	assert.NotContains(t, data, "to_balance_after")
}
