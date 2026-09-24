package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func readRouter(h *harness) http.Handler {
	reads := handlers.NewReadHandler(h.service)
	r := chi.NewRouter()
	r.Get("/payments/{id}", reads.GetPayment)
	r.Get("/accounts/{account_id}/payments", reads.ListAccountPayments)
	return r
}

func get(handler http.Handler, path string) (*httptest.ResponseRecorder, map[string]interface{}) {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec, tests.FromJson(rec.Body.String())
}

func TestGetPayment(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusCompleted)
	payment.Description = "rent"
	payment.ExternalID = "ted_1"
	balance := int64(5750)
	payment.BalanceAfterCents = &balance
	payment.CompletedAt = &now
	h.repo.Put(payment)

	rec, body := get(readRouter(h), "/payments/"+payment.ID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, payment.ID.String(), data["payment_id"])
	assert.Equal(t, payment.AccountID.String(), data["account_id"])
	assert.Equal(t, "ted", data["payment_method"])
	assert.Equal(t, "completed", data["status"])
	assert.Equal(t, "42.50", data["amount"])
	assert.Equal(t, "BRL", data["currency"])
	assert.Equal(t, "Ana", data["recipient"])
	assert.Equal(t, map[string]interface{}{"bank_code": "341", "branch": "0001", "account": "123456", "document": "*********25"}, data["ted"])
	assert.Equal(t, "rent", data["description"])
	assert.Equal(t, "k", data["idempotency_key"])
	assert.Equal(t, "ted_1", data["external_id"])
	assert.Equal(t, "57.50", data["balance_after"])
	assert.NotNil(t, data["completed_at"])
	assert.NotContains(t, data, "pix_key")
	assert.NotContains(t, data, "boleto_code")
	assert.NotContains(t, data, "failure_reason")

	pending := h.stored(models.MethodBoleto, models.StatusRefunding)
	pending.FailureReason = "boleto_not_found"
	h.repo.Put(pending)
	_, body = get(readRouter(h), "/payments/"+pending.ID.String())
	data = body["data"].(map[string]interface{})
	assert.Equal(t, boleto150, data["boleto_code"])
	assert.Equal(t, "boleto_not_found", data["failure_reason"])
	assert.NotContains(t, data, "ted")
	assert.NotContains(t, data, "balance_after")
	assert.NotContains(t, data, "completed_at")
	assert.NotContains(t, data, "external_id")

	rec, body = get(readRouter(h), "/payments/nope")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "uuid", body["error"].(map[string]interface{})["details"].(map[string]interface{})["id"])

	rec, body = get(readRouter(h), "/payments/"+uuid.NewString())
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "PAYMENT_NOT_FOUND", body["error"].(map[string]interface{})["code"])

	h.repo.Err = errors.New("db down")
	rec, _ = get(readRouter(h), "/payments/"+payment.ID.String())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetPaymentMasksShortTedDocuments(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusCompleted)
	payment.TED.Document = "1"
	h.repo.Put(payment)

	_, body := get(readRouter(h), "/payments/"+payment.ID.String())
	data := body["data"].(map[string]interface{})
	ted := data["ted"].(map[string]interface{})
	assert.Equal(t, "*", ted["document"])

	payment = h.stored(models.MethodTED, models.StatusCompleted)
	payment.TED.Document = "12"
	h.repo.Put(payment)

	_, body = get(readRouter(h), "/payments/"+payment.ID.String())
	data = body["data"].(map[string]interface{})
	ted = data["ted"].(map[string]interface{})
	assert.Equal(t, "**", ted["document"])
}

func TestListAccountPaymentsPagesWithBeforeCursor(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	base := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	payments := make([]*models.Payment, 3)
	for i := 0; i < 3; i++ {
		p := &models.Payment{ID: uuid.New(), AccountID: account, Method: models.MethodPix, Status: models.StatusCompleted, AmountCents: int64(1000 + i), Currency: "BRL", Recipient: "Ana", PixKey: "ana@example.com", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute)}
		h.repo.Put(p)
		payments[i] = p
	}

	rec, body := get(readRouter(h), "/accounts/"+account.String()+"/payments?limit=2")
	assert.Equal(t, http.StatusOK, rec.Code)
	items := body["data"].([]interface{})
	assert.Len(t, items, 2)
	assert.Equal(t, payments[2].ID.String(), items[0].(map[string]interface{})["payment_id"])
	assert.Equal(t, payments[1].ID.String(), items[1].(map[string]interface{})["payment_id"])
	nextBefore := rec.Header().Get("X-Next-Before")
	assert.Equal(t, payments[1].CreatedAt.UTC().Format(time.RFC3339Nano), nextBefore)

	rec, body = get(readRouter(h), "/accounts/"+account.String()+"/payments?limit=2&before="+nextBefore)
	assert.Equal(t, http.StatusOK, rec.Code)
	items = body["data"].([]interface{})
	assert.Len(t, items, 1)
	assert.Equal(t, payments[0].ID.String(), items[0].(map[string]interface{})["payment_id"])
	assert.Empty(t, rec.Header().Get("X-Next-Before"))

	rec, body = get(readRouter(h), "/accounts/"+account.String()+"/payments?before=not-a-date")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "datetime", body["error"].(map[string]interface{})["details"].(map[string]interface{})["before"])

	rec, _ = get(readRouter(h), "/accounts/"+account.String()+"/payments?before=2026-09-20T10%3A00%3A00.123456789Z")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListAccountPaymentsReturnsNextBeforeWhenARecordIsNotYetVisible(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	base := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	payments := make([]*models.Payment, 2)
	for i := 0; i < 2; i++ {
		p := &models.Payment{ID: uuid.New(), AccountID: account, Method: models.MethodPix, Status: models.StatusCompleted, AmountCents: int64(1000 + i), Currency: "BRL", Recipient: "Ana", PixKey: "ana@example.com", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute)}
		h.repo.Put(p)
		payments[i] = p
	}
	h.repo.Invisible = map[uuid.UUID]bool{payments[0].ID: true}

	rec, body := get(readRouter(h), "/accounts/"+account.String()+"/payments?limit=2")

	assert.Equal(t, http.StatusOK, rec.Code)
	items := body["data"].([]interface{})
	assert.Len(t, items, 1)
	assert.Equal(t, payments[1].ID.String(), items[0].(map[string]interface{})["payment_id"])
	assert.Equal(t, payments[0].CreatedAt.UTC().Format(time.RFC3339Nano), rec.Header().Get("X-Next-Before"))
}

func TestListAccountPayments(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusCompleted)

	rec, body := get(readRouter(h), "/accounts/"+payment.AccountID.String()+"/payments")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 1)
	assert.Equal(t, "ana@example.com", body["data"].([]interface{})[0].(map[string]interface{})["pix_key"])

	for _, limit := range []string{"1", "200"} {
		rec, _ = get(readRouter(h), "/accounts/"+payment.AccountID.String()+"/payments?limit="+limit)
		assert.Equal(t, http.StatusOK, rec.Code, limit)
	}
	for _, bad := range []string{"0", "201", "x", "-1"} {
		rec, body = get(readRouter(h), "/accounts/"+payment.AccountID.String()+"/payments?limit="+bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, bad)
		assert.Equal(t, "range", body["error"].(map[string]interface{})["details"].(map[string]interface{})["limit"], bad)
	}

	rec, body = get(readRouter(h), "/accounts/x/payments")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "uuid", body["error"].(map[string]interface{})["details"].(map[string]interface{})["account_id"])

	h.repo.Err = errors.New("db down")
	rec, _ = get(readRouter(h), "/accounts/"+payment.AccountID.String()+"/payments")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
