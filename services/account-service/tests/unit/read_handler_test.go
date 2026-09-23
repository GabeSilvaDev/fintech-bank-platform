package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func uuidString() string { return uuid.NewString() }

func readRouter(h *harness) http.Handler {
	reads := handlers.NewReadHandler(h.service)
	r := chi.NewRouter()
	r.Get("/accounts/{id}", reads.GetAccount)
	r.Get("/accounts/{id}/owner", reads.GetOwner)
	r.Get("/users/{user_id}/accounts", reads.ListUserAccounts)
	return r
}

func get(handler http.Handler, path string) (*httptest.ResponseRecorder, map[string]interface{}) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, tests.FromJson(rec.Body.String())
}

func TestGetAccountReturnsAccount(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1050)

	rec, body := get(readRouter(h), "/accounts/"+account.AccountID.String())

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, account.AccountID.String(), data["account_id"])
	assert.Equal(t, account.UserID.String(), data["user_id"])
	assert.Equal(t, "0001", data["agency"])
	assert.Equal(t, "00000001", data["number"])
	assert.Equal(t, "checking", data["type"])
	assert.Equal(t, "active", data["status"])
	assert.Equal(t, "BRL", data["currency"])
	assert.Equal(t, 10.5, data["balance"])
	assert.NotContains(t, data, "closed_at")
}

func TestGetAccountIncludesClosedAt(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	closedAt := now
	account.Status = "closed"
	account.ClosedAt = &closedAt
	h.accounts.Put(account)

	_, body := get(readRouter(h), "/accounts/"+account.AccountID.String())

	assert.Contains(t, body["data"].(map[string]interface{}), "closed_at")
}

func TestGetAccountErrors(t *testing.T) {
	h := newHarness()

	rec, body := get(readRouter(h), "/accounts/not-a-uuid")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", body["error"].(map[string]interface{})["code"])

	rec, body = get(readRouter(h), "/accounts/"+uuidString())
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "ACCOUNT_NOT_FOUND", body["error"].(map[string]interface{})["code"])

	h.accounts.Err = errors.New("db down")
	rec, body = get(readRouter(h), "/accounts/"+uuidString())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_ERROR", body["error"].(map[string]interface{})["code"])
}

func TestGetOwnerReturnsContactDetails(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)

	rec, body := get(readRouter(h), "/accounts/"+account.AccountID.String()+"/owner")

	assert.Equal(t, http.StatusOK, rec.Code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, account.AccountID.String(), data["account_id"])
	assert.Equal(t, account.UserID.String(), data["user_id"])
	assert.Equal(t, "Ana Souza", data["name"])
	assert.Equal(t, "ana@example.com", data["email"])
	assert.Equal(t, "11999887766", data["phone"])
}

func TestGetOwnerOmitsEmptyPhone(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	customer := h.customers.Customers[account.UserID]
	customer.Phone = ""
	h.customers.Put(customer)

	_, body := get(readRouter(h), "/accounts/"+account.AccountID.String()+"/owner")

	assert.NotContains(t, body["data"].(map[string]interface{}), "phone")
}

func TestGetOwnerMissingCustomerReturnsNotFound(t *testing.T) {
	h := newHarness()
	account := &models.Account{AccountID: uuid.New(), UserID: uuid.New(), Agency: "0001", Number: "00000004", Type: models.AccountTypeChecking, Status: models.AccountStatusActive, Currency: "BRL", CreatedAt: now, UpdatedAt: now}
	h.accounts.Put(account)

	rec, body := get(readRouter(h), "/accounts/"+account.AccountID.String()+"/owner")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "ACCOUNT_NOT_FOUND", body["error"].(map[string]interface{})["code"])
}

func TestGetOwnerErrors(t *testing.T) {
	h := newHarness()

	rec, body := get(readRouter(h), "/accounts/not-a-uuid/owner")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", body["error"].(map[string]interface{})["code"])

	rec, body = get(readRouter(h), "/accounts/"+uuidString()+"/owner")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "ACCOUNT_NOT_FOUND", body["error"].(map[string]interface{})["code"])

	h.accounts.Err = errors.New("db down")
	rec, body = get(readRouter(h), "/accounts/"+uuidString()+"/owner")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_ERROR", body["error"].(map[string]interface{})["code"])
}

func TestListUserAccounts(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	rec, body := get(readRouter(h), "/users/"+account.UserID.String()+"/accounts")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 1)

	rec, body = get(readRouter(h), "/users/"+uuidString()+"/accounts")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body["data"].([]interface{}), 0)

	rec, _ = get(readRouter(h), "/users/x/accounts")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	h.accounts.Err = errors.New("db down")
	rec, _ = get(readRouter(h), "/users/"+uuidString()+"/accounts")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
