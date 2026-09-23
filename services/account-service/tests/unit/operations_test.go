package unit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/stretchr/testify/assert"
)

func TestCreditFirstCallReservesAppliesAndCompletes(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.NoError(t, err)
	assert.NotNil(t, result.Credited)
	assert.Equal(t, 1, h.operations.ReserveCalls)
	assert.Equal(t, 1, h.operations.CompleteCalls)

	operation, ok := h.operations.Operations[account.AccountID.String()+"/k-1"]
	assert.True(t, ok)
	assert.Equal(t, models.OperationDone, operation.Status)
	assert.Equal(t, "credit", operation.Kind)

	var stored services.CreditResult
	assert.NoError(t, json.Unmarshal([]byte(operation.Result), &stored))
	assert.NotNil(t, stored.Credited)
	assert.Equal(t, result.Credited.BalanceAfter, stored.Credited.BalanceAfter)
	assert.True(t, result.Credited.OccurredAt.Equal(stored.Credited.OccurredAt))
}

func TestCreditSecondCallWithSameKeyReplaysWithoutTouchingBalance(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	first, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))
	assert.NoError(t, err)

	second, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))
	assert.NoError(t, err)

	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.NotNil(t, second.Credited)
	assert.Equal(t, first.Credited.BalanceAfter, second.Credited.BalanceAfter)
	assert.True(t, first.Credited.OccurredAt.Equal(second.Credited.OccurredAt))
	assert.Equal(t, int64(1000), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestDebitRejectionIsReplayedWithoutRereadingTheBalance(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)

	first, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 5))
	assert.NoError(t, err)
	assert.Equal(t, "insufficient_funds", first.Rejected.Reason)

	h.accounts.GetErrs = []error{errors.New("should not be called")}
	second, err := h.service.Debit(context.Background(), debit(account.AccountID.String(), 5))
	assert.NoError(t, err)
	assert.NotNil(t, second.Rejected)
	assert.Equal(t, first.Rejected.Reason, second.Rejected.Reason)
	assert.Equal(t, first.Rejected.Balance, second.Rejected.Balance)
}

func TestCreditWithPendingKeyIsAmbiguous(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	reserved, err := h.operations.Reserve(context.Background(), account.AccountID, "k-1", "credit", now)
	assert.NoError(t, err)
	assert.True(t, reserved)

	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))
	assert.True(t, errors.Is(err, domain.ErrAmbiguousWrite))
}

func TestKeyReusedForDifferentKindIsRejected(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(1000)

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))
	assert.NoError(t, err)

	_, err = h.service.Debit(context.Background(), events.DebitAccountPayload{AccountID: account.AccountID.String(), Amount: 5, Currency: "BRL", IdempotencyKey: "k-1"})
	assert.Equal(t, "idempotency_key_reused", domain.InvalidCode(err))
}

func TestKeyReuseIsReportedEvenWhilePending(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	reserved, err := h.operations.Reserve(context.Background(), account.AccountID, "k-1", "credit", now)
	assert.NoError(t, err)
	assert.True(t, reserved)

	_, err = h.service.Debit(context.Background(), events.DebitAccountPayload{AccountID: account.AccountID.String(), Amount: 5, Currency: "BRL", IdempotencyKey: "k-1"})
	assert.Equal(t, "idempotency_key_reused", domain.InvalidCode(err))
}

func TestCreditRequiresIdempotencyKey(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	_, err := h.service.Credit(context.Background(), events.CreditAccountPayload{AccountID: account.AccountID.String(), Amount: 1, Currency: "BRL", IdempotencyKey: "   "})
	assert.Equal(t, "invalid_idempotency_key", domain.InvalidCode(err))

	_, err = h.service.Debit(context.Background(), events.DebitAccountPayload{AccountID: account.AccountID.String(), Amount: 1, Currency: "BRL"})
	assert.Equal(t, "invalid_idempotency_key", domain.InvalidCode(err))
}

func TestConflictFromCASReleasesTheKeyForRetry(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Equal(t, 1, h.operations.ReleaseCalls)
	_, ok := h.operations.Operations[account.AccountID.String()+"/k-1"]
	assert.False(t, ok)

	h.accounts.CASResults = nil
	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))
	assert.NoError(t, err)
	assert.NotNil(t, result.Credited)
}

func TestAmbiguousCASErrorKeepsTheKeyPending(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{Err: fmt.Errorf("%w: timeout", domain.ErrAmbiguousWrite)}}

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.True(t, errors.Is(err, domain.ErrAmbiguousWrite))
	assert.Equal(t, 0, h.operations.ReleaseCalls)
	operation, ok := h.operations.Operations[account.AccountID.String()+"/k-1"]
	assert.True(t, ok)
	assert.Equal(t, models.OperationPending, operation.Status)
}

func TestReleaseFailureIsReturnedInsteadOfTheOriginalError(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}
	h.operations.ReleaseErr = errors.New("release boom")

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.EqualError(t, err, "release boom")
}

func TestCompleteFailureStillReturnsTheResult(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.operations.CompleteErr = errors.New("complete boom")

	result, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.NoError(t, err)
	assert.NotNil(t, result.Credited)

	h.operations.CompleteErr = nil
	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.True(t, errors.Is(err, domain.ErrAmbiguousWrite))
	assert.Equal(t, 1, h.accounts.CASCalls)
	assert.Equal(t, int64(1000), h.accounts.Accounts[account.AccountID].BalanceCents)
}

func TestReleaseRunsEvenWhenTheCommandContextIsCancelled(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CASResults = []tests.CASResult{{}, {}, {}, {}, {}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.service.Credit(ctx, credit(account.AccountID.String(), 10))

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Equal(t, 1, h.operations.ReleaseCalls)
	assert.NoError(t, h.operations.ReleaseCtxErr)
	assert.WithinDuration(t, time.Now().Add(5*time.Second), h.operations.ReleaseDeadline, time.Second)
	_, ok := h.operations.Operations[account.AccountID.String()+"/k-1"]
	assert.False(t, ok)
}

func TestKeyReleasedBetweenReserveAndGetIsARetryableConflict(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	reserved, err := h.operations.Reserve(context.Background(), account.AccountID, "k-1", "credit", now)
	assert.NoError(t, err)
	assert.True(t, reserved)
	h.operations.GetErr = domain.ErrNotFound

	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.NotErrorIs(t, err, domain.ErrNotFound)
	assert.ErrorContains(t, err, "k-1")
	assert.Equal(t, 0, h.accounts.CASCalls)
}

func TestReserveErrorPropagates(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.operations.ReserveErr = errors.New("reserve boom")

	_, err := h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.EqualError(t, err, "reserve boom")
}

func TestGetErrorPropagatesWhenKeyIsAlreadyReserved(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	reserved, err := h.operations.Reserve(context.Background(), account.AccountID, "k-1", "credit", now)
	assert.NoError(t, err)
	assert.True(t, reserved)
	h.operations.GetErr = errors.New("get boom")

	_, err = h.service.Credit(context.Background(), credit(account.AccountID.String(), 10))

	assert.EqualError(t, err, "get boom")
}
