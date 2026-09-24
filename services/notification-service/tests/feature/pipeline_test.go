package feature

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/app/services"
	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/notification-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var (
	ana   = models.Contact{AccountID: uuid.New(), UserID: uuid.New(), Name: "Ana Souza", Email: "ana@example.com", Phone: "+5511999887766"}
	bruno = models.Contact{AccountID: uuid.New(), UserID: uuid.New(), Name: "Bruno Lima", Email: "bruno@example.com"}
)

type pipeline struct {
	directory *tests.FakeDirectory
	publisher *tests.FakePublisher
	history   *tests.FakeHistory
	email     *tests.FakeSender
	sms       *tests.FakeSender
	push      *tests.FakeSender
	routing   *processor.Processor
	delivery  *processor.Processor
	cursor    int
}

func newPipeline(t *testing.T, contacts ...models.Contact) *pipeline {
	renderer, err := services.NewRenderer(services.Templates())
	assert.NoError(t, err)

	p := &pipeline{
		directory: tests.NewFakeDirectory(contacts...),
		publisher: &tests.FakePublisher{},
		history:   &tests.FakeHistory{},
		email:     &tests.FakeSender{},
		sms:       &tests.FakeSender{},
		push:      &tests.FakeSender{},
	}
	log := logger.New(logger.Config{Output: &bytes.Buffer{}})
	router := services.NewRouter(p.directory, renderer, tests.FakeClock{T: time.Now().UTC()}, time.Hour, log)
	senders := map[models.Channel]contracts.Sender{models.ChannelEmail: p.email, models.ChannelSMS: p.sms, models.ChannelPush: p.push}
	delivery := services.NewDelivery(senders, p.history, tests.FakeClock{T: time.Now().UTC()}, log)

	cfg := processor.Config{Source: "notification-service", FailedEventType: events.EventTypes.NotificationCommandFailed, DLQTopic: events.Topics.NotificationDLQ, Backoff: []time.Duration{time.Millisecond}}
	p.routing = processor.NewProcessor(router, tests.NewFakeStore(), p.publisher, cfg, log)
	p.delivery = processor.NewProcessor(delivery, tests.NewFakeStore(), p.publisher, cfg, log)
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
		if published.Topic == events.Topics.NotificationEvents {
			p.send(t, p.delivery, published.Event)
		}
	}
}

func (p *pipeline) recordsFor(userID uuid.UUID) []models.Record {
	var records []models.Record
	for _, record := range p.history.Records {
		if record.UserID == userID {
			records = append(records, record)
		}
	}
	return records
}

func (p *pipeline) dlq() []tests.PublishedEvent {
	return p.publisher.ByTopic(events.Topics.NotificationDLQ)
}

func TestAccountCreatedEmailsTheOwner(t *testing.T) {
	p := newPipeline(t, ana)
	source := events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: ana.AccountID.String(), UserID: ana.UserID.String(), AccountNumber: "12345678", Agency: "0001", AccountType: "checking"})

	p.send(t, p.routing, source)

	assert.Len(t, p.email.Sent, 1)
	assert.Equal(t, "ana@example.com", p.email.Sent[0].To)
	assert.Equal(t, "Bem-vindo(a) ao Fintech Bank", p.email.Sent[0].Subject)
	assert.Empty(t, p.sms.Sent)
	assert.Empty(t, p.push.Sent)

	records := p.recordsFor(ana.UserID)
	assert.Len(t, records, 1)
	assert.Equal(t, source.ID, records[0].SourceEventID)

	assert.Empty(t, p.dlq())
}

func TestTransferCompletedPushesBothParties(t *testing.T) {
	p := newPipeline(t, ana, bruno)
	source := events.NewTransactionEvent(events.EventTypes.TransferCompleted, events.TransferCompletedPayload{FromAccountID: ana.AccountID.String(), ToAccountID: bruno.AccountID.String(), Amount: domain.AmountFromCents(3000), FromBalanceAfter: domain.AmountFromCents(7000), ToBalanceAfter: domain.AmountFromCents(3000)})

	p.send(t, p.routing, source)

	assert.Len(t, p.push.Sent, 2)
	assert.Empty(t, p.email.Sent)
	assert.Len(t, p.recordsFor(ana.UserID), 1)
	assert.Len(t, p.recordsFor(bruno.UserID), 1)
	assert.Empty(t, p.dlq())
}

func TestRefundFailedEscalatesBySMSWhenAvailable(t *testing.T) {
	p := newPipeline(t, ana, bruno)

	p.send(t, p.routing, events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(1000), Reason: "pix_key_not_found", Status: "refund_failed"}))
	assert.Len(t, p.push.Sent, 1)
	assert.Len(t, p.email.Sent, 1)
	assert.Len(t, p.sms.Sent, 1)
	assert.Equal(t, ana.Phone, p.sms.Sent[0].To)

	p.send(t, p.routing, events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{AccountID: bruno.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(1000), Reason: "pix_key_not_found", Status: "refund_failed"}))
	assert.Len(t, p.push.Sent, 2)
	assert.Len(t, p.email.Sent, 2)
	assert.Len(t, p.sms.Sent, 1)

	assert.Empty(t, p.dlq())
}

func TestFailingPushIsDeadLetteredButEmailStillGoesOut(t *testing.T) {
	p := newPipeline(t, ana)
	p.push.Errs = []error{errors.New("fcm down"), errors.New("fcm down")}

	p.send(t, p.routing, events.NewPaymentEvent(events.EventTypes.PaymentCompleted, events.PaymentCompletedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(4250)}))

	assert.Empty(t, p.push.Sent)
	assert.Len(t, p.email.Sent, 1)

	dlq := p.dlq()
	assert.Len(t, dlq, 1)
	assert.Equal(t, events.EventTypes.NotificationCommandFailed, dlq[0].Event.Type)
	errPayload := dlq[0].Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "internal_error", errPayload.ErrorCode)
}

func TestReplayingTheSameEventSendsNothingNew(t *testing.T) {
	p := newPipeline(t, ana)
	source := events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: ana.AccountID.String(), UserID: ana.UserID.String(), AccountNumber: "12345678", Agency: "0001", AccountType: "checking"})

	p.send(t, p.routing, source)
	assert.Len(t, p.email.Sent, 1)

	p.send(t, p.routing, source)
	assert.Len(t, p.email.Sent, 1)
	assert.Len(t, p.recordsFor(ana.UserID), 1)
}

func TestUnknownAccountIsIgnored(t *testing.T) {
	p := newPipeline(t, ana)

	p.send(t, p.routing, events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: uuid.NewString(), UserID: uuid.NewString(), AccountNumber: "1", Agency: "1", AccountType: "checking"}))

	assert.Empty(t, p.publisher.Published)
	assert.Empty(t, p.email.Sent)
	assert.Empty(t, p.sms.Sent)
	assert.Empty(t, p.push.Sent)
}
