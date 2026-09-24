package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func credit(id string, cents int64) events.CreditAccountPayload {
	return events.CreditAccountPayload{AccountID: id, Amount: domain.AmountFromCents(cents), Currency: "BRL", Reference: "tx-1", IdempotencyKey: "k-1"}
}

func debit(id string, cents int64) events.DebitAccountPayload {
	return events.DebitAccountPayload{AccountID: id, Amount: domain.AmountFromCents(cents), Currency: "brl", Reference: "tx-2", IdempotencyKey: "k-2"}
}

func TestCreditIncreasesBalance(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 1050))
	credited := result.Credited

	assert.NoError(t, err)
	assert.Nil(t, result.Rejected)
	assert.Equal(t, domain.AmountFromCents(1050), credited.Amount)
	assert.Equal(t, domain.AmountFromCents(1050), credited.BalanceAfter)
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

	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))
	credited := result.Credited

	assert.NoError(t, err)
	assert.Nil(t, result.Rejected)
	assert.Equal(t, domain.AmountFromCents(200), credited.BalanceAfter)
	assert.Equal(t, 2, h.accounts.CASCalls)
}

func TestCreditGivesUpAfterFiveConflicts(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Equal(t, 5, h.accounts.CASCalls)
}

func TestCreditRejections(t *testing.T) {
	h := newHarness()
	active := h.activeAccount(0)

	cases := map[string]events.CreditAccountPayload{
		"invalid_account_id":   credit("x", 100),
		"invalid_amount":       credit(active.AccountID.String(), 0),
		"unsupported_currency": {AccountID: active.AccountID.String(), Amount: domain.AmountFromCents(100), Currency: "USD"},
	}
	for code, cmd := range cases {
		_, err := h.service.Credit(context.Background(), cmd)
		assert.Equal(t, code, domain.InvalidCode(err), code)
	}
}

func TestCreditRejectsInactiveAndMissingAccounts(t *testing.T) {
	h := newHarness()
	blocked := h.activeAccount(300)
	blocked.Status = models.AccountStatusBlocked
	h.accounts.Put(blocked)

	result, err := h.service.Credit(context.Background(), credit(blocked.AccountID.String(), 100))
	assert.NoError(t, err)
	assert.Nil(t, result.Credited)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, domain.AmountFromCents(300), result.Rejected.Balance)
	assert.Equal(t, "tx-1", result.Rejected.Reference)
	assert.Equal(t, "k-1", result.Rejected.IdempotencyKey)

	missing := uuid.NewString()
	result, err = h.service.Credit(context.Background(), credit(missing, 100))
	assert.NoError(t, err)
	assert.Equal(t, "account_not_found", result.Rejected.Reason)
	assert.Equal(t, missing, result.Rejected.AccountID)
	assert.Equal(t, domain.AmountFromCents(0), result.Rejected.Balance)
	assert.Equal(t, 0, h.accounts.CASCalls)
}

func TestCreditRejectsWhenAccountIsBlockedBeforeCAS(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusBlocked }

	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))

	assert.NoError(t, err)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(100), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestCreditIsRefusedByRepositoryWhenAccountIsNotActive(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusBlocked }

	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))

	assert.NoError(t, err)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(100), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestCreditPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{Err: errors.New("db down")}}
	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))
	assert.EqualError(t, err, "db down")

	h = newHarness()
	account = h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.GetErrs = []error{nil, errors.New("db down")}
	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))
	assert.EqualError(t, err, "db down")

	h = newHarness()
	account = h.activeAccount(0)
	h.accounts.GetErrs = []error{errors.New("db down")}
	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 100))
	assert.EqualError(t, err, "db down")
}

func TestBalanceAmountsAreCarriedExactly(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)

	creditResult, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 30))
	assert.NoError(t, err)
	assert.Nil(t, creditResult.Rejected)
	assert.Equal(t, domain.AmountFromCents(30), creditResult.Credited.Amount)
	assert.Equal(t, domain.AmountFromCents(130), creditResult.Credited.BalanceAfter)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 30))
	assert.NoError(t, err)
	assert.Equal(t, domain.AmountFromCents(30), result.Debited.Amount)
	assert.Equal(t, domain.AmountFromCents(100), result.Debited.BalanceAfter)

	result, err = h.service.Debit(context.Background(), events.DebitAccountPayload{AccountID: account.AccountID.String(), Amount: domain.AmountFromCents(130), Currency: "brl", Reference: "tx-2", IdempotencyKey: "k-3"})
	assert.NoError(t, err)
	assert.Equal(t, "insufficient_funds", result.Rejected.Reason)
	assert.Equal(t, domain.AmountFromCents(130), result.Rejected.Amount)
}

func TestDebitDecreasesBalance(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 300))

	assert.NoError(t, err)
	assert.Nil(t, result.Rejected)
	assert.Equal(t, domain.AmountFromCents(300), result.Debited.Amount)
	assert.Equal(t, domain.AmountFromCents(700), result.Debited.BalanceAfter)
	assert.Equal(t, "tx-2", result.Debited.Reference)
	assert.Equal(t, now, result.Debited.OccurredAt)
	assert.Equal(t, int64(700), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRejectsInsufficientFunds(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(250)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 300))

	assert.NoError(t, err)
	assert.Nil(t, result.Debited)
	assert.Equal(t, "insufficient_funds", result.Rejected.Reason)
	assert.Equal(t, domain.AmountFromCents(250), result.Rejected.Balance)
	assert.Equal(t, domain.AmountFromCents(300), result.Rejected.Amount)
	assert.Equal(t, "k-2", result.Rejected.IdempotencyKey)
	assert.Equal(t, 0, h.accounts.CASCalls)
	assert.Equal(t, int64(250), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRejectsInactiveAccount(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	account.Status = models.AccountStatusBlocked
	h.accounts.Put(account)

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))

	assert.NoError(t, err)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, domain.AmountFromCents(1000), result.Rejected.Balance)
}

func TestDebitRejectsWhenAccountIsBlockedBeforeCAS(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusBlocked }

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))

	assert.NoError(t, err)
	assert.Nil(t, result.Debited)
	assert.Equal(t, "account_not_active", result.Rejected.Reason)
	assert.Equal(t, domain.AmountFromCents(1000), result.Rejected.Balance)
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(1000), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRechecksBalanceAfterConflict(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.OnCAS = func() { h.accounts.Accounts[account.AccountID].BalanceCents = 100 }

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 500))

	assert.NoError(t, err)
	assert.Nil(t, result.Debited)
	assert.Equal(t, "insufficient_funds", result.Rejected.Reason)
	assert.Equal(t, domain.AmountFromCents(100), result.Rejected.Balance)
	assert.Equal(t, domain.AmountFromCents(500), result.Rejected.Amount)
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(100), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRetriesThenSucceeds(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{Applied: false}, {Applied: false}}

	result, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))

	assert.NoError(t, err)
	assert.Equal(t, domain.AmountFromCents(900), result.Debited.BalanceAfter)
	assert.Equal(t, 3, h.accounts.CASCalls)
}

func TestDebitGivesUpAfterFiveConflicts(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}

	_, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))

	assert.ErrorIs(t, err, domain.ErrConflict)
}

func TestDebitValidationAndErrors(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)

	_, err := h.service.Debit(context.Background(), debit("x", 100))
	assert.Equal(t, "invalid_account_id", domain.InvalidCode(err))

	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), -100))
	assert.Equal(t, "invalid_amount", domain.InvalidCode(err))
	assert.EqualError(t, err, "invalid_amount: amount must be greater than zero")

	_, err = h.service.Debit(context.Background(), events.DebitAccountPayload{AccountID: account.AccountID.String(), Amount: domain.AmountFromCents(100), Currency: "EUR"})
	assert.Equal(t, "unsupported_currency", domain.InvalidCode(err))

	h.accounts.GetErrs = []error{errors.New("db down")}
	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))
	assert.EqualError(t, err, "db down")

	h.accounts.CASResults = []tests.CASResult{{Err: errors.New("db down")}}
	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))
	assert.EqualError(t, err, "db down")

	h.accounts.CASResults = []tests.CASResult{{Applied: false}}
	h.accounts.GetErrs = []error{nil, errors.New("db down")}
	_, err = h.service.Debit(context.Background(), debit(account.AccountID.String(), 100))
	assert.EqualError(t, err, "db down")
}

func TestDebitRejectsMissingAccount(t *testing.T) {
	h := newHarness()
	missing := uuid.NewString()

	result, err := h.service.Debit(context.Background(), debit(missing, 100))

	assert.NoError(t, err)
	assert.Equal(t, "account_not_found", result.Rejected.Reason)
	assert.Equal(t, missing, result.Rejected.AccountID)
}
