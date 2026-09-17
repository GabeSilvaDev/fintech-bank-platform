package feature

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/handlers"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type pipeline struct {
	repo      *tests.FakeTransactionRepo
	publisher *tests.FakePublisher
	commands  *processor.Processor
	replies   *processor.Processor
}

func newPipeline() *pipeline {
	p := &pipeline{repo: tests.NewFakeTransactionRepo(), publisher: &tests.FakePublisher{}}
	log := logger.New(logger.Config{Output: &bytes.Buffer{}})
	service := services.NewTransactionService(p.repo, tests.FakeClock{T: time.Now().UTC()}, uuid.New)
	cfg := processor.Config{Source: "transaction-service", FailedEventType: events.EventTypes.TransactionCommandFailed, DLQTopic: events.Topics.TransactionDLQ, Backoff: []time.Duration{time.Millisecond}}
	p.commands = processor.NewProcessor(handlers.NewCommandDispatcher(service, log), tests.NewFakeStore(), p.publisher, cfg, log)
	p.replies = processor.NewProcessor(handlers.NewReplyDispatcher(service), tests.NewFakeStore(), p.publisher, cfg, log)
	return p
}

func (p *pipeline) send(t *testing.T, proc *processor.Processor, event *events.Event) {
	raw, err := event.ToJSON()
	assert.NoError(t, err)
	assert.NoError(t, proc.Process(context.Background(), []byte("k"), raw))
}

func (p *pipeline) lastCommand(t *testing.T) (*events.Event, string) {
	commands := p.publisher.ByTopic(events.Topics.AccountCommands)
	assert.NotEmpty(t, commands)
	last := commands[len(commands)-1]
	return last.Event, last.Key
}

func replyTo(command *events.Event, kind string, balance float64, reason string) *events.Event {
	var reference, key, account string
	switch payload := command.Payload.(type) {
	case events.CreditAccountPayload:
		reference, key, account = payload.Reference, payload.IdempotencyKey, payload.AccountID
	case events.DebitAccountPayload:
		reference, key, account = payload.Reference, payload.IdempotencyKey, payload.AccountID
	}
	switch kind {
	case events.EventTypes.AccountCredited:
		return events.NewAccountEvent(kind, events.AccountCreditedPayload{AccountID: account, BalanceAfter: balance, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
	case events.EventTypes.AccountDebited:
		return events.NewAccountEvent(kind, events.AccountDebitedPayload{AccountID: account, BalanceAfter: balance, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
	case events.EventTypes.DebitRejected:
		return events.NewAccountEvent(kind, events.DebitRejectedPayload{AccountID: account, Reason: reason, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
	}
	return events.NewAccountEvent(kind, events.CreditRejectedPayload{AccountID: account, Reason: reason, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
}

func TestTransferSagaThroughProcessors(t *testing.T) {
	p := newPipeline()
	from, to := uuid.NewString(), uuid.NewString()

	p.send(t, p.commands, events.NewTransactionCommand(events.EventTypes.ProcessTransfer, events.ProcessTransferPayload{FromAccountID: from, ToAccountID: to, Amount: 30, Currency: "BRL", IdempotencyKey: "tr-1"}).WithTraceID("t-1"))
	created := p.publisher.ByTopic(events.Topics.TransactionEvents)
	assert.Len(t, created, 1)
	assert.Equal(t, events.EventTypes.TransactionCreated, created[0].Event.Type)
	debit, key := p.lastCommand(t)
	assert.Equal(t, events.EventTypes.DebitAccount, debit.Type)
	assert.Equal(t, from, key)

	p.send(t, p.replies, replyTo(debit, events.EventTypes.AccountDebited, 70, ""))
	credit, key := p.lastCommand(t)
	assert.Equal(t, events.EventTypes.CreditAccount, credit.Type)
	assert.Equal(t, to, key)
	assert.Equal(t, "t-1", credit.TraceID)

	p.send(t, p.replies, replyTo(credit, events.EventTypes.AccountCredited, 30, ""))
	results := p.publisher.ByTopic(events.Topics.TransactionEvents)
	assert.Len(t, results, 2)
	assert.Equal(t, events.EventTypes.TransferCompleted, results[1].Event.Type)
	completed := results[1].Event.Payload.(events.TransferCompletedPayload)
	assert.Equal(t, 70.0, completed.FromBalanceAfter)
	assert.Equal(t, 30.0, completed.ToBalanceAfter)
	assert.Equal(t, models.StatusCompleted, p.repo.Transactions[p.repo.Created[0].ID].Status)
	assert.Empty(t, p.publisher.ByTopic(events.Topics.TransactionDLQ))

	p.send(t, p.commands, events.NewTransactionCommand(events.EventTypes.ProcessTransfer, events.ProcessTransferPayload{FromAccountID: from, ToAccountID: to, Amount: 30, Currency: "BRL", IdempotencyKey: "tr-1"}))
	assert.Len(t, p.publisher.ByTopic(events.Topics.TransactionEvents), 2)
}

func TestTransferCompensationThroughProcessors(t *testing.T) {
	p := newPipeline()
	from, to := uuid.NewString(), uuid.NewString()
	p.send(t, p.commands, events.NewTransactionCommand(events.EventTypes.ProcessTransfer, events.ProcessTransferPayload{FromAccountID: from, ToAccountID: to, Amount: 30, Currency: "BRL", IdempotencyKey: "tr-2"}))
	debit, _ := p.lastCommand(t)
	p.send(t, p.replies, replyTo(debit, events.EventTypes.AccountDebited, 70, ""))
	credit, _ := p.lastCommand(t)

	p.send(t, p.replies, replyTo(credit, events.EventTypes.CreditRejected, 0, "account_not_active"))
	reversal, key := p.lastCommand(t)
	assert.Equal(t, from, key)
	assert.Equal(t, models.StepKey(p.repo.Created[0].ID, models.StepReversal), reversal.Payload.(events.CreditAccountPayload).IdempotencyKey)

	p.send(t, p.replies, replyTo(reversal, events.EventTypes.AccountCredited, 100, ""))
	results := p.publisher.ByTopic(events.Topics.TransactionEvents)
	assert.Equal(t, events.EventTypes.TransferFailed, results[len(results)-1].Event.Type)
	assert.Equal(t, "reversed", results[len(results)-1].Event.Payload.(events.TransferFailedPayload).Status)
	assert.Equal(t, models.StatusReversed, p.repo.Transactions[p.repo.Created[0].ID].Status)
	assert.Empty(t, p.publisher.ByTopic(events.Topics.TransactionDLQ))
}

func TestDepositRejectedAndUnrelatedEvents(t *testing.T) {
	p := newPipeline()
	account := uuid.NewString()
	p.send(t, p.commands, events.NewTransactionCommand(events.EventTypes.CreateTransaction, events.CreateTransactionPayload{AccountID: account, Type: "deposit", Amount: 5, Currency: "BRL", IdempotencyKey: "dep-9"}))
	credit, _ := p.lastCommand(t)

	p.send(t, p.replies, events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: account}))
	p.send(t, p.replies, replyTo(credit, events.EventTypes.CreditRejected, 0, "account_not_found"))

	results := p.publisher.ByTopic(events.Topics.TransactionEvents)
	assert.Equal(t, events.EventTypes.TransactionFailed, results[len(results)-1].Event.Type)
	assert.Equal(t, models.StatusFailed, p.repo.Transactions[p.repo.Created[0].ID].Status)
	assert.Empty(t, p.publisher.ByTopic(events.Topics.TransactionDLQ))
}
