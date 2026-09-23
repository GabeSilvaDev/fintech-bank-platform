package feature

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type pipeline struct {
	repo      *tests.FakePaymentRepo
	gateway   *tests.FakeGateway
	publisher *tests.FakePublisher
	commands  *processor.Processor
	replies   *processor.Processor
	cursor    int
}

func newPipeline() *pipeline {
	p := &pipeline{repo: tests.NewFakePaymentRepo(), gateway: &tests.FakeGateway{}, publisher: &tests.FakePublisher{}}
	log := logger.New(logger.Config{Output: &bytes.Buffer{}})
	service := services.NewPaymentService(p.repo, p.gateway, tests.FakeClock{T: time.Now().UTC()}, uuid.New)
	cfg := processor.Config{Source: "payment-service", FailedEventType: events.EventTypes.PaymentCommandFailed, DLQTopic: events.Topics.PaymentDLQ, Backoff: []time.Duration{time.Millisecond}}
	p.commands = processor.NewProcessor(handlers.NewCommandDispatcher(service, log), tests.NewFakeStore(), p.publisher, cfg, log)
	p.replies = processor.NewProcessor(handlers.NewReplyDispatcher(service, log), tests.NewFakeStore(), p.publisher, cfg, log)
	return p
}

func (p *pipeline) send(t *testing.T, proc *processor.Processor, event *events.Event) {
	raw, err := event.ToJSON()
	assert.NoError(t, err)
	assert.NoError(t, proc.Process(context.Background(), []byte("k"), raw))
	p.pump(t)
}

func (p *pipeline) pump(t *testing.T) {
	for p.cursor < len(p.publisher.Published) {
		published := p.publisher.Published[p.cursor]
		p.cursor++
		if published.Topic == events.Topics.PaymentCommands {
			p.send(t, p.commands, published.Event)
		}
	}
}

func (p *pipeline) lastAccountCommand(t *testing.T) *events.Event {
	commands := p.publisher.ByTopic(events.Topics.AccountCommands)
	assert.NotEmpty(t, commands)
	return commands[len(commands)-1].Event
}

func (p *pipeline) lastResult(t *testing.T) *events.Event {
	results := p.publisher.ByTopic(events.Topics.PaymentEvents)
	assert.NotEmpty(t, results)
	return results[len(results)-1].Event
}

func (p *pipeline) only(t *testing.T) *models.Payment {
	assert.Len(t, p.repo.Created, 1)
	return p.repo.Payments[p.repo.Created[0].ID]
}

func answer(command *events.Event, kind string, balance float64, reason string) *events.Event {
	var account, reference, key string
	switch payload := command.Payload.(type) {
	case events.DebitAccountPayload:
		account, reference, key = payload.AccountID, payload.Reference, payload.IdempotencyKey
	case events.CreditAccountPayload:
		account, reference, key = payload.AccountID, payload.Reference, payload.IdempotencyKey
	}
	var payload interface{}
	switch kind {
	case events.EventTypes.AccountDebited:
		payload = events.AccountDebitedPayload{AccountID: account, BalanceAfter: balance, Reference: reference, IdempotencyKey: key}
	case events.EventTypes.AccountCredited:
		payload = events.AccountCreditedPayload{AccountID: account, BalanceAfter: balance, Reference: reference, IdempotencyKey: key}
	case events.EventTypes.DebitRejected:
		payload = events.DebitRejectedPayload{AccountID: account, Reason: reason, Reference: reference, IdempotencyKey: key}
	default:
		payload = events.CreditRejectedPayload{AccountID: account, Reason: reason, Reference: reference, IdempotencyKey: key}
	}
	return events.NewAccountEvent(kind, payload).WithTraceID(command.TraceID)
}

func process(accountID string, method string, key string) *events.Event {
	payload := events.ProcessPaymentPayload{AccountID: accountID, PaymentMethod: method, Amount: 150, Currency: "BRL", Recipient: "Energia SA", IdempotencyKey: key}
	switch method {
	case "pix":
		payload.PixKey = "ana@example.com"
	case "boleto":
		payload.BoletoCode = "34191790010100000012334567812309811000000015000"
	case "ted":
		payload.TED = &events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}
	}
	return events.NewPaymentCommand(events.EventTypes.ProcessPayment, payload).WithTraceID("t-" + key)
}

func TestPixSettlesSynchronously(t *testing.T) {
	p := newPipeline()
	account := uuid.NewString()
	p.send(t, p.commands, process(account, "pix", "pix-1"))
	debit := p.lastAccountCommand(t)
	assert.Equal(t, events.EventTypes.DebitAccount, debit.Type)

	p.send(t, p.replies, answer(debit, events.EventTypes.AccountDebited, 850, ""))

	assert.Equal(t, events.EventTypes.PaymentCompleted, p.lastResult(t).Type)
	assert.Equal(t, "t-pix-1", p.lastResult(t).TraceID)
	payment := p.only(t)
	assert.Equal(t, models.StatusCompleted, payment.Status)
	assert.Equal(t, int64(85000), *payment.BalanceAfterCents)
	assert.Len(t, p.gateway.Calls, 1)

	p.send(t, p.commands, process(account, "pix", "pix-1"))
	assert.Len(t, p.repo.Created, 1)
	assert.Empty(t, p.publisher.ByTopic(events.Topics.PaymentDLQ))
}

func TestTedSettlesThroughTheWebhook(t *testing.T) {
	p := newPipeline()
	p.gateway.Submissions = []models.Submission{{ExternalID: "ted_1", Status: models.SubmissionPending}}
	p.send(t, p.commands, process(uuid.NewString(), "ted", "ted-1"))
	p.send(t, p.replies, answer(p.lastAccountCommand(t), events.EventTypes.AccountDebited, 850, ""))

	assert.Equal(t, events.EventTypes.PaymentProcessed, p.lastResult(t).Type)
	assert.Equal(t, models.StatusSubmitted, p.only(t).Status)

	settle := events.NewEvent(events.EventTypes.SettlePayment, "payment-service", events.SettlePaymentPayload{ExternalID: "ted_1", Status: "settled"})
	p.send(t, p.commands, settle)
	p.send(t, p.commands, settle.WithTraceID("again"))

	assert.Equal(t, events.EventTypes.PaymentCompleted, p.lastResult(t).Type)
	assert.Equal(t, models.StatusCompleted, p.only(t).Status)
	assert.Len(t, p.publisher.ByTopic(events.Topics.PaymentEvents), 3)
	assert.Empty(t, p.publisher.ByTopic(events.Topics.PaymentDLQ))
}

func TestBoletoRejectedByTheProviderIsRefunded(t *testing.T) {
	p := newPipeline()
	p.gateway.Submissions = []models.Submission{{ExternalID: "bol_1", Status: models.SubmissionPending}}
	p.send(t, p.commands, process(uuid.NewString(), "boleto", "bol-1"))
	p.send(t, p.replies, answer(p.lastAccountCommand(t), events.EventTypes.AccountDebited, 850, ""))
	p.send(t, p.commands, events.NewEvent(events.EventTypes.SettlePayment, "payment-service", events.SettlePaymentPayload{ExternalID: "bol_1", Status: "rejected", Reason: "boleto_not_found"}))

	refund := p.lastAccountCommand(t)
	assert.Equal(t, events.EventTypes.CreditAccount, refund.Type)
	assert.Equal(t, models.StatusRefunding, p.only(t).Status)

	p.send(t, p.replies, answer(refund, events.EventTypes.AccountCredited, 1000, ""))

	failed := p.lastResult(t)
	assert.Equal(t, events.EventTypes.PaymentFailed, failed.Type)
	assert.Equal(t, "refunded", failed.Payload.(events.PaymentFailedPayload).Status)
	assert.Equal(t, "boleto_not_found", failed.Payload.(events.PaymentFailedPayload).Reason)
	assert.Equal(t, models.StatusRefunded, p.only(t).Status)
}

func TestRejectedPixWhoseRefundFailsIsEscalated(t *testing.T) {
	p := newPipeline()
	p.gateway.Submissions = []models.Submission{{Status: models.SubmissionRejected, Reason: "pix_key_not_found"}}
	p.send(t, p.commands, process(uuid.NewString(), "pix", "pix-2"))
	p.send(t, p.replies, answer(p.lastAccountCommand(t), events.EventTypes.AccountDebited, 850, ""))
	p.send(t, p.replies, answer(p.lastAccountCommand(t), events.EventTypes.CreditRejected, 0, "account_not_active"))

	assert.Equal(t, models.StatusRefundFailed, p.only(t).Status)
	dlq := p.publisher.ByTopic(events.Topics.PaymentDLQ)
	assert.Len(t, dlq, 1)
	assert.Equal(t, "refund_failed", dlq[0].Event.Payload.(events.ErrorPayload).ErrorCode)
}

func TestInsufficientFundsFailsThePayment(t *testing.T) {
	p := newPipeline()
	p.send(t, p.commands, process(uuid.NewString(), "ted", "ted-2"))
	p.send(t, p.replies, answer(p.lastAccountCommand(t), events.EventTypes.DebitRejected, 0, "insufficient_funds"))

	assert.Equal(t, "failed", p.lastResult(t).Payload.(events.PaymentFailedPayload).Status)
	assert.Equal(t, models.StatusFailed, p.only(t).Status)
	assert.Empty(t, p.gateway.Calls)
}

func TestEarlySettlementIsRetriedThenDeadLettered(t *testing.T) {
	p := newPipeline()
	id := uuid.New()
	p.repo.Put(&models.Payment{ID: id, AccountID: uuid.New(), Method: models.MethodTED, Status: models.StatusDebited, AmountCents: 100, Currency: "BRL"})
	p.repo.External["ted_9"] = id

	p.send(t, p.commands, events.NewEvent(events.EventTypes.SettlePayment, "payment-service", events.SettlePaymentPayload{ExternalID: "ted_9", Status: "settled"}))

	dlq := p.publisher.ByTopic(events.Topics.PaymentDLQ)
	assert.Len(t, dlq, 1)
	assert.Equal(t, events.EventTypes.PaymentCommandFailed, dlq[0].Event.Type)
	assert.Equal(t, "conflict", dlq[0].Event.Payload.(events.ErrorPayload).ErrorCode)
	assert.Equal(t, models.StatusDebited, p.repo.Payments[id].Status)
}
