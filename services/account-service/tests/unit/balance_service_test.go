package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func credit(id string, amount float64) events.CreditAccountPayload {
	return events.CreditAccountPayload{AccountID: id, Amount: amount, Currency: "BRL", Reference: "tx-1", IdempotencyKey: "k-1"}
}

func debit(id string, amount float64) events.DebitAccountPayload {
	return events.DebitAccountPayload{AccountID: id, Amount: amount, Currency: "brl", Reference: "tx-2", IdempotencyKey: "k-2"}
}

func TestCreditIncreasesBalance(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	credited, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10.5))

	assert.NoError(t, err)
	assert.Equal(t, 10.5, credited.Amount)
	assert.Equal(t, 10.5, credited.BalanceAfter)
	assert.Equal(t, "tx-1", credited.Reference)
	assert.Equal(t, "k-1", credited.IdempotencyKey)
	assert.Equal(t, now, credited.OccurredAt)
	assert.Equal(t, int64(1050), h.accounts.Accounts[account.AccountID].BalanceCents)
	assert.Equal(t, 1, h.accounts.CASCalls)
}

func TestCreditRetriesAfterConcurrentUpdate(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}

	credited, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 1))

	assert.NoError(t, err)
	assert.Equal(t, 2.0, credited.BalanceAfter)
	assert.Equal(t, 2, h.accounts.CASCalls)
}

func TestCreditGivesUpAfterFiveConflicts(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 1))

	assert.ErrorIs(t, err, models.ErrConflict)
	assert.Equal(t, 5, h.accounts.CASCalls)
}

func TestCreditRejections(t *testing.T) {
	h := newHarness()
	blocked := h.activeAccount(0)
	blocked.Status = models.AccountStatusBlocked
	h.accounts.Put(blocked)
	active := h.activeAccount(0)

	cases := map[string]events.CreditAccountPayload{
		"invalid_account_id":   credit("x", 1),
		"invalid_amount":       credit(active.AccountID.String(), 0),
		"unsupported_currency": {AccountID: active.AccountID.String(), Amount: 1, Currency: "USD"},
		"account_not_active":   credit(blocked.AccountID.String(), 1),
	}
	for code, cmd := range cases {
		_, err := h.service.Credit(context.Background(), cmd)
		assert.Equal(t, code, models.InvalidCode(err), code)
	}

	_, err := h.service.Credit(context.Background(), credit(uuid.NewString(), 1))
	assert.ErrorIs(t, err, models.ErrNotFound)
	assert.Equal(t, 0, h.accounts.CASCalls)
}

func TestCreditRejectsWhenAccountIsBlockedBeforeCAS(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusBlocked }

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 1))

	assert.Equal(t, "account_not_active", models.InvalidCode(err))
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(100), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestCreditIsRefusedByRepositoryWhenAccountIsNotActive(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusBlocked }

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 1))

	assert.Equal(t, "account_not_active", models.InvalidCode(err))
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(100), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestCreditPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{Err: errors.New("db down")}}
	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 1))
	assert.EqualError(t, err, "db down")

	h = newHarness()
	account = h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.GetErrs = []error{nil, errors.New("db down")}
	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 1))
	assert.EqualError(t, err, "db down")
}

func TestDebitDecreasesBalance(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 3))

	assert.NoError(t, err)
	assert.Nil(t, result.Rejected)
	assert.Equal(t, 3.0, result.Debited.Amount)
	assert.Equal(t, 7.0, result.Debited.BalanceAfter)
	assert.Equal(t, "tx-2", result.Debited.Reference)
	assert.Equal(t, now, result.Debited.OccurredAt)
	assert.Equal(t, int64(700), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRejectsInsufficientFunds(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(250)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 3))

	assert.NoError(t, err)
	assert.Nil(t, result.Debited)
	assert.Equal(t, "insufficient_funds", result.Rejected.Reason)
	assert.Equal(t, 2.5, result.Rejected.Balance)
	assert.Equal(t, 3.0, result.Rejected.Amount)
	assert.Equal(t, "k-2", result.Rejected.IdempotencyKey)
	assert.Equal(t, 0, h.accounts.CASCalls)
	assert.Equal(t, int64(250), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRejectsInactiveAccount(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	account.Status = models.AccountStatusBlocked
	h.accounts.Put(account)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 1))

	assert.NoError(t, err)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, 10.0, result.Rejected.Balance)
}

func TestDebitRejectsWhenAccountIsBlockedBeforeCAS(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusBlocked }

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 1))

	assert.NoError(t, err)
	assert.Nil(t, result.Debited)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, 10.0, result.Rejected.Balance)
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(1000), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRechecksBalanceAfterConflict(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.Accounts[account.AccountID].BalanceCents = 100

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 5))

	assert.NoError(t, err)
	assert.Equal(t, "insufficient_funds", result.Rejected.Reason)
	assert.Equal(t, 1.0, result.Rejected.Balance)
}

func TestDebitRetriesThenSucceeds(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}, {Applied: false}}

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 1))

	assert.NoError(t, err)
	assert.Equal(t, 9.0, result.Debited.BalanceAfter)
	assert.Equal(t, 3, h.accounts.CASCalls)
}

func TestDebitGivesUpAfterFiveConflicts(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}

	_, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 1))

	assert.ErrorIs(t, err, models.ErrConflict)
}

func TestDebitValidationAndErrors(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)

	_, err := h.service.Debit(context.Background(), debit("x", 1))
	assert.Equal(t, "invalid_account_id", models.InvalidCode(err))

	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), 1.005))
	assert.Equal(t, "invalid_amount", models.InvalidCode(err))

	_, err = h.service.Debit(context.Background(), events.DebitAccountPayload{AccountID: account.AccountID.String(), Amount: 1, Currency: "EUR"})
	assert.Equal(t, "unsupported_currency", models.InvalidCode(err))

	_, err = h.service.Debit(context.Background(), debit(uuid.NewString(), 1))
	assert.ErrorIs(t, err, models.ErrNotFound)

	h.accounts.CASResults = []tests.CASResult{{Err: errors.New("db down")}}
	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), 1))
	assert.EqualError(t, err, "db down")

	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.GetErrs = []error{nil, errors.New("db down")}
	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), 1))
	assert.EqualError(t, err, "db down")
}
