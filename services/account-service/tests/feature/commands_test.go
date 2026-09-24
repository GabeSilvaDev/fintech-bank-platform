package feature

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestCommandsEndToEndWithFakeInfrastructure(t *testing.T) {
	accounts := tests.NewFakeAccountRepo()
	customers := tests.NewFakeCustomerRepo()
	publisher := &tests.FakePublisher{}
	operations := tests.NewFakeOperationRepo()
	service := services.NewAccountService(accounts, customers, operations, tests.FakeClock{T: time.Now().UTC()}, func() string { return "87654321" })
	proc := processor.NewProcessor(handlers.NewDispatcher(service), tests.NewFakeStore(), publisher, processor.Config{
		Source:          "account-service",
		FailedEventType: events.EventTypes.AccountCommandFailed,
		DLQTopic:        events.Topics.AccountDLQ,
		Backoff:         []time.Duration{time.Millisecond},
	}, logger.New(logger.Config{Output: &bytes.Buffer{}}))
	ctx := context.Background()

	create := events.NewAccountCommand(events.EventTypes.CreateAccount, events.CreateAccountPayload{UserID: uuid.NewString(), AccountType: "savings", Name: "Bruno Costa", Email: "bruno@example.com", Document: "52998224725"}).WithTraceID("t-1")
	raw, _ := create.ToJSON()
	assert.NoError(t, proc.Process(ctx, []byte("u"), raw))

	created := publisher.ByTopic(events.Topics.AccountEvents)[0].Event.Payload.(events.AccountCreatedPayload)
	assert.Equal(t, "87654321", created.AccountNumber)
	accountID := uuid.MustParse(created.AccountID)
	assert.Equal(t, models.AccountStatusActive, accounts.Accounts[accountID].Status)

	debit := events.NewAccountCommand(events.EventTypes.DebitAccount, events.DebitAccountPayload{AccountID: created.AccountID, Amount: domain.AmountFromCents(1000), Currency: "BRL", IdempotencyKey: "d-1"}).WithTraceID("t-2")
	raw, _ = debit.ToJSON()
	assert.NoError(t, proc.Process(ctx, []byte(created.AccountID), raw))

	published := publisher.ByTopic(events.Topics.AccountEvents)
	assert.Equal(t, events.EventTypes.DebitRejected, published[1].Event.Type)
	assert.Equal(t, "t-2", published[1].Event.TraceID)

	closeWithBalance := events.NewAccountCommand(events.EventTypes.DeleteAccount, events.DeleteAccountPayload{AccountID: created.AccountID})
	accounts.Accounts[accountID].BalanceCents = 5
	raw, _ = closeWithBalance.ToJSON()
	assert.NoError(t, proc.Process(ctx, []byte(created.AccountID), raw))

	dlq := publisher.ByTopic(events.Topics.AccountDLQ)
	assert.Len(t, dlq, 1)
	assert.Equal(t, "account_has_balance", dlq[0].Event.Payload.(events.ErrorPayload).ErrorCode)
}
