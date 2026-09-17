package unit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

type harness struct {
	repo    *tests.FakeTransactionRepo
	nextID  uuid.UUID
	service *services.TransactionService
}

func newHarness() *harness {
	h := &harness{repo: tests.NewFakeTransactionRepo(), nextID: uuid.New()}
	h.service = services.NewTransactionService(h.repo, tests.FakeClock{T: now}, func() uuid.UUID { return h.nextID })
	return h
}

func deposit(accountID string) events.CreateTransactionPayload {
	return events.CreateTransactionPayload{AccountID: accountID, Type: "deposit", Amount: 100.25, Currency: "BRL", Description: "salary", IdempotencyKey: "dep-1"}
}

func transfer(from, to string) events.ProcessTransferPayload {
	return events.ProcessTransferPayload{FromAccountID: from, ToAccountID: to, Amount: 30, Currency: "brl", Description: "rent", IdempotencyKey: "tr-1"}
}

func TestCreateDepositRecordsAndRequestsCredit(t *testing.T) {
	h := newHarness()
	account := uuid.New()

	res, err := h.service.Create(context.Background(), deposit(account.String()), "trace-1")

	assert.NoError(t, err)
	assert.Len(t, res.Messages, 2)

	tx := h.repo.Created[0]
	assert.Equal(t, h.nextID, tx.ID)
	assert.Equal(t, models.TypeDeposit, tx.Type)
	assert.Equal(t, models.StatusPending, tx.Status)
	assert.Equal(t, account, tx.AccountID)
	assert.Nil(t, tx.CounterpartyID)
	assert.Equal(t, int64(10025), tx.AmountCents)
	assert.Equal(t, "BRL", tx.Currency)
	assert.Equal(t, "dep-1", tx.IdempotencyKey)
	assert.Equal(t, now, tx.CreatedAt)
	assert.Equal(t, h.nextID, h.repo.Keys["dep-1"])

	created := res.Messages[0]
	assert.Equal(t, events.Topics.TransactionEvents, created.Topic)
	assert.Equal(t, account.String(), created.Key)
	assert.Equal(t, events.EventTypes.TransactionCreated, created.Event.Type)
	assert.Equal(t, "transaction-service", created.Event.Source)
	assert.Equal(t, "trace-1", created.Event.TraceID)
	payload := created.Event.Payload.(events.TransactionCreatedPayload)
	assert.Equal(t, h.nextID.String(), payload.TransactionID)
	assert.Equal(t, "deposit", payload.Type)
	assert.Equal(t, 100.25, payload.Amount)
	assert.Equal(t, "", payload.CounterpartyID)

	command := res.Messages[1]
	assert.Equal(t, events.Topics.AccountCommands, command.Topic)
	assert.Equal(t, account.String(), command.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, command.Event.Type)
	assert.Equal(t, "transaction-service", command.Event.Source)
	assert.Equal(t, "trace-1", command.Event.TraceID)
	credit := command.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, account.String(), credit.AccountID)
	assert.Equal(t, 100.25, credit.Amount)
	assert.Equal(t, "BRL", credit.Currency)
	assert.Equal(t, h.nextID.String(), credit.Reference)
	assert.Equal(t, h.nextID.String()+":credit", credit.IdempotencyKey)
}

func TestCreateWithdrawalRequestsDebit(t *testing.T) {
	h := newHarness()
	cmd := deposit(uuid.NewString())
	cmd.Type = "withdrawal"

	res, err := h.service.Create(context.Background(), cmd, "trace-2")

	assert.NoError(t, err)
	assert.Equal(t, models.TypeWithdrawal, h.repo.Created[0].Type)
	assert.Equal(t, events.EventTypes.DebitAccount, res.Messages[1].Event.Type)
	debit := res.Messages[1].Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, h.nextID.String()+":debit", debit.IdempotencyKey)
	assert.Equal(t, h.nextID.String(), debit.Reference)
}

func TestCreateValidation(t *testing.T) {
	cases := map[string]func(*events.CreateTransactionPayload){
		"invalid_account_id":      func(c *events.CreateTransactionPayload) { c.AccountID = "x" },
		"invalid_type":            func(c *events.CreateTransactionPayload) { c.Type = "transfer" },
		"invalid_amount":          func(c *events.CreateTransactionPayload) { c.Amount = 0 },
		"unsupported_currency":    func(c *events.CreateTransactionPayload) { c.Currency = "USD" },
		"invalid_idempotency_key": func(c *events.CreateTransactionPayload) { c.IdempotencyKey = "   " },
		"invalid_description":     func(c *events.CreateTransactionPayload) { c.Description = strings.Repeat("x", 256) },
	}
	for code, mutate := range cases {
		h := newHarness()
		cmd := deposit(uuid.NewString())
		mutate(&cmd)

		_, err := h.service.Create(context.Background(), cmd, "t")

		assert.Equal(t, code, domain.InvalidCode(err), code)
		assert.Empty(t, h.repo.Created, code)
		assert.Empty(t, h.repo.Keys, code)
	}

	h := newHarness()
	cmd := deposit(uuid.NewString())
	cmd.IdempotencyKey = strings.Repeat("k", 65)
	_, err := h.service.Create(context.Background(), cmd, "t")
	assert.Equal(t, "invalid_idempotency_key", domain.InvalidCode(err))
}

func TestCreateDuplicateKeyIsNoOp(t *testing.T) {
	h := newHarness()
	h.repo.Keys["dep-1"] = uuid.New()

	res, err := h.service.Create(context.Background(), deposit(uuid.NewString()), "t")

	assert.ErrorIs(t, err, models.ErrDuplicateKey)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.repo.Created)
}

func TestCreatePropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	h.repo.ReserveErr = errors.New("db down")
	_, err := h.service.Create(context.Background(), deposit(uuid.NewString()), "t")
	assert.EqualError(t, err, "db down")

	h = newHarness()
	h.repo.CreateErr = errors.New("db down")
	_, err = h.service.Create(context.Background(), deposit(uuid.NewString()), "t")
	assert.EqualError(t, err, "db down")
}

func TestTransferRecordsAndRequestsDebit(t *testing.T) {
	h := newHarness()
	from, to := uuid.New(), uuid.New()

	res, err := h.service.Transfer(context.Background(), transfer(from.String(), to.String()), "trace-3")

	assert.NoError(t, err)
	tx := h.repo.Created[0]
	assert.Equal(t, models.TypeTransfer, tx.Type)
	assert.Equal(t, from, tx.AccountID)
	assert.Equal(t, to, *tx.CounterpartyID)
	assert.Equal(t, int64(3000), tx.AmountCents)
	assert.Equal(t, "BRL", tx.Currency)
	assert.Equal(t, "rent", tx.Description)

	assert.Len(t, res.Messages, 2)
	assert.Equal(t, from.String(), res.Messages[0].Key)
	assert.Equal(t, to.String(), res.Messages[0].Event.Payload.(events.TransactionCreatedPayload).CounterpartyID)
	assert.Equal(t, events.EventTypes.DebitAccount, res.Messages[1].Event.Type)
	assert.Equal(t, from.String(), res.Messages[1].Key)
	debit := res.Messages[1].Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, from.String(), debit.AccountID)
	assert.Equal(t, 30.0, debit.Amount)
	assert.Equal(t, h.nextID.String()+":debit", debit.IdempotencyKey)
	assert.Equal(t, "trace-3", res.Messages[1].Event.TraceID)
}

func TestTransferValidation(t *testing.T) {
	same := uuid.NewString()
	cases := map[string]events.ProcessTransferPayload{
		"invalid_from_account_id": transfer("x", uuid.NewString()),
		"invalid_to_account_id":   transfer(uuid.NewString(), "x"),
		"same_account":            transfer(same, same),
		"invalid_amount":          {FromAccountID: uuid.NewString(), ToAccountID: uuid.NewString(), Amount: 1.005, Currency: "BRL", IdempotencyKey: "k"},
		"unsupported_currency":    {FromAccountID: uuid.NewString(), ToAccountID: uuid.NewString(), Amount: 1, Currency: "EUR", IdempotencyKey: "k"},
		"invalid_idempotency_key": {FromAccountID: uuid.NewString(), ToAccountID: uuid.NewString(), Amount: 1, Currency: "BRL"},
	}
	for code, cmd := range cases {
		h := newHarness()
		_, err := h.service.Transfer(context.Background(), cmd, "t")
		assert.Equal(t, code, domain.InvalidCode(err), code)
		assert.Empty(t, h.repo.Created, code)
	}

	h := newHarness()
	h.repo.Keys["tr-1"] = uuid.New()
	_, err := h.service.Transfer(context.Background(), transfer(uuid.NewString(), uuid.NewString()), "t")
	assert.ErrorIs(t, err, models.ErrDuplicateKey)
}

func TestGetAndListByAccount(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	_, _ = h.service.Create(context.Background(), deposit(account.String()), "t")

	tx, err := h.service.Get(context.Background(), h.nextID)
	assert.NoError(t, err)
	assert.Equal(t, account, tx.AccountID)

	list, err := h.service.ListByAccount(context.Background(), account, 10)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestSystemClock(t *testing.T) {
	assert.WithinDuration(t, time.Now(), services.SystemClock{}.Now(), time.Second)
}
