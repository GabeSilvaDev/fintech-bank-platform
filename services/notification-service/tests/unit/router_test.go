package unit

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/app/services"
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

func newRouter(t *testing.T, directory *tests.FakeDirectory) *services.Router {
	r, err := services.NewRenderer(services.Templates())
	assert.NoError(t, err)
	return services.NewRouter(directory, r, tests.FakeClock{T: time.Now().UTC()}, time.Hour, logger.New(logger.Config{Output: &bytes.Buffer{}}))
}

func agedRouter(t *testing.T, now time.Time, maxAge time.Duration, logs *bytes.Buffer) *services.Router {
	r, err := services.NewRenderer(services.Templates())
	assert.NoError(t, err)
	return services.NewRouter(tests.NewFakeDirectory(ana), r, tests.FakeClock{T: now}, maxAge, logger.New(logger.Config{Output: logs}))
}

func paymentCompletedAt(at time.Time) *events.Event {
	event := events.NewPaymentEvent(events.EventTypes.PaymentCompleted, events.PaymentCompletedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(1000)})
	event.Timestamp = at
	return event
}

func TestRouterRoutesFreshEvents(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	logs := &bytes.Buffer{}
	router := agedRouter(t, now, time.Hour, logs)

	assert.Len(t, route(t, router, paymentCompletedAt(now.Add(-59*time.Minute))).Messages, 2)
	assert.Len(t, route(t, router, paymentCompletedAt(now.Add(-time.Hour))).Messages, 2)
	assert.Len(t, route(t, router, paymentCompletedAt(time.Time{})).Messages, 2)
	assert.Empty(t, logs.String())
}

func TestRouterSkipsStaleEvents(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	logs := &bytes.Buffer{}
	directory := tests.NewFakeDirectory(ana)
	r, err := services.NewRenderer(services.Templates())
	assert.NoError(t, err)
	router := services.NewRouter(directory, r, tests.FakeClock{T: now}, time.Hour, logger.New(logger.Config{Output: logs}))
	stale := paymentCompletedAt(now.Add(-2 * time.Hour))

	assert.Empty(t, route(t, router, stale).Messages)
	assert.Equal(t, 0, directory.Calls)
	assert.Contains(t, logs.String(), `"message":"stale event skipped"`)
	assert.Contains(t, logs.String(), `"event_id":"`+stale.ID+`"`)
	assert.Contains(t, logs.String(), `"event_type":"`+events.EventTypes.PaymentCompleted+`"`)
	assert.Contains(t, logs.String(), `"age":`)
}

func TestRouterZeroMaxAgeDisablesTheStalenessGuard(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	logs := &bytes.Buffer{}
	router := agedRouter(t, now, 0, logs)

	assert.Len(t, route(t, router, paymentCompletedAt(now.Add(-30*24*time.Hour))).Messages, 2)
	assert.Empty(t, logs.String())
}

func route(t *testing.T, router *services.Router, event *events.Event) processor.Result {
	res, err := router.Dispatch(context.Background(), event)
	assert.NoError(t, err)
	return res
}

func commandTypes(res processor.Result) []string {
	types := []string{}
	for _, msg := range res.Messages {
		types = append(types, msg.Event.Type)
	}
	return types
}

func TestWelcomeEmail(t *testing.T) {
	source := events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: ana.AccountID.String(), UserID: ana.UserID.String(), AccountNumber: "12345678", Agency: "0001", AccountType: "checking"}).WithTraceID("trace-1")

	res := route(t, newRouter(t, tests.NewFakeDirectory(ana)), source)

	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.NotificationEvents, msg.Topic)
	assert.Equal(t, ana.UserID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.SendEmail, msg.Event.Type)
	assert.Equal(t, "notification-service", msg.Event.Source)
	assert.Equal(t, "trace-1", msg.Event.TraceID)
	assert.Equal(t, ana.UserID.String(), msg.Event.Metadata["user_id"])
	assert.Equal(t, source.ID, msg.Event.Metadata["source_event_id"])
	email := msg.Event.Payload.(events.SendEmailPayload)
	assert.Equal(t, "ana@example.com", email.To)
	assert.Equal(t, "Bem-vindo(a) ao Fintech Bank", email.Subject)
	assert.Equal(t, "welcome", email.Template)
	assert.Contains(t, email.Data["body"], "Olá, Ana Souza!")
	assert.Contains(t, email.Data["body"], "conta corrente (agência 0001, número 12345678)")
	assert.Equal(t, models.PriorityNormal, email.Priority)
}

func TestTransactionNotifications(t *testing.T) {
	router := newRouter(t, tests.NewFakeDirectory(ana))

	completed := route(t, router, events.NewTransactionEvent(events.EventTypes.TransactionCompleted, events.TransactionCompletedPayload{AccountID: ana.AccountID.String(), Type: "deposit", Amount: domain.AmountFromCents(10000), BalanceAfter: domain.AmountFromCents(15000)}))
	assert.Equal(t, []string{events.EventTypes.SendPush}, commandTypes(completed))
	push := completed.Messages[0].Event.Payload.(events.SendPushPayload)
	assert.Equal(t, ana.UserID.String(), push.UserID)
	assert.Equal(t, "Depósito concluído", push.Title)
	assert.Equal(t, "Depósito de R$ 100,00 concluído. Saldo atual: R$ 150,00.", push.Body)
	assert.Equal(t, "transaction_completed", push.Data["kind"])
	assert.Equal(t, models.PriorityNormal, push.Priority)

	failed := route(t, router, events.NewTransactionEvent(events.EventTypes.TransactionFailed, events.TransactionFailedPayload{AccountID: ana.AccountID.String(), Type: "withdrawal", Amount: domain.AmountFromCents(1000), Reason: "insufficient_funds"}))
	assert.Equal(t, []string{events.EventTypes.SendPush, events.EventTypes.SendEmail}, commandTypes(failed))
	assert.Equal(t, "Seu saque de R$ 10,00 não foi realizado: saldo insuficiente.", failed.Messages[0].Event.Payload.(events.SendPushPayload).Body)
	assert.Equal(t, models.PriorityHigh, failed.Messages[1].Event.Payload.(events.SendEmailPayload).Priority)
}

func TestTransferNotifications(t *testing.T) {
	router := newRouter(t, tests.NewFakeDirectory(ana, bruno))

	completed := route(t, router, events.NewTransactionEvent(events.EventTypes.TransferCompleted, events.TransferCompletedPayload{FromAccountID: ana.AccountID.String(), ToAccountID: bruno.AccountID.String(), Amount: domain.AmountFromCents(3000), FromBalanceAfter: domain.AmountFromCents(7000), ToBalanceAfter: domain.AmountFromCents(3000)}))
	assert.Len(t, completed.Messages, 2)
	assert.Equal(t, ana.UserID.String(), completed.Messages[0].Key)
	assert.Equal(t, "Você enviou R$ 30,00. Saldo atual: R$ 70,00.", completed.Messages[0].Event.Payload.(events.SendPushPayload).Body)
	assert.Equal(t, bruno.UserID.String(), completed.Messages[1].Key)
	assert.Equal(t, "Você recebeu R$ 30,00. Saldo atual: R$ 30,00.", completed.Messages[1].Event.Payload.(events.SendPushPayload).Body)

	failed := route(t, router, events.NewTransactionEvent(events.EventTypes.TransferFailed, events.TransferFailedPayload{FromAccountID: ana.AccountID.String(), ToAccountID: bruno.AccountID.String(), Amount: domain.AmountFromCents(3000), Reason: "account_not_active", Status: "reversed"}))
	assert.Equal(t, []string{events.EventTypes.SendPush, events.EventTypes.SendEmail}, commandTypes(failed))
	assert.Equal(t, "Sua transferência de R$ 30,00 não foi concluída: conta inativa. O valor foi devolvido à sua conta.", failed.Messages[0].Event.Payload.(events.SendPushPayload).Body)

	unknownReceiver := route(t, router, events.NewTransactionEvent(events.EventTypes.TransferCompleted, events.TransferCompletedPayload{FromAccountID: ana.AccountID.String(), ToAccountID: uuid.NewString(), Amount: domain.AmountFromCents(500), FromBalanceAfter: domain.AmountFromCents(6500)}))
	assert.Len(t, unknownReceiver.Messages, 1)
}

func TestPaymentNotifications(t *testing.T) {
	router := newRouter(t, tests.NewFakeDirectory(ana, bruno))

	completed := route(t, router, events.NewPaymentEvent(events.EventTypes.PaymentCompleted, events.PaymentCompletedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(4250)}))
	assert.Equal(t, []string{events.EventTypes.SendPush, events.EventTypes.SendEmail}, commandTypes(completed))
	assert.Equal(t, "Seu pagamento via PIX de R$ 42,50 foi concluído.", completed.Messages[0].Event.Payload.(events.SendPushPayload).Body)

	refunded := route(t, router, events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "boleto", Amount: domain.AmountFromCents(15000), Reason: "boleto_not_found", Status: "refunded"}))
	assert.Equal(t, []string{events.EventTypes.SendPush, events.EventTypes.SendEmail}, commandTypes(refunded))
	assert.Equal(t, "Seu pagamento via boleto de R$ 150,00 não foi concluído: boleto não encontrado. O valor foi estornado para sua conta.", refunded.Messages[1].Event.Payload.(events.SendEmailPayload).Data["body"])

	stuck := route(t, router, events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(1000), Reason: "pix_key_not_found", Status: "refund_failed"}))
	assert.Equal(t, []string{events.EventTypes.SendPush, events.EventTypes.SendEmail, events.EventTypes.SendSMS}, commandTypes(stuck))
	sms := stuck.Messages[2].Event.Payload.(events.SendSMSPayload)
	assert.Equal(t, "+5511999887766", sms.To)
	assert.Contains(t, sms.Message, "nossa equipe entrará em contato")
	assert.Equal(t, models.PriorityHigh, sms.Priority)

	noPhone := route(t, router, events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{AccountID: bruno.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(1000), Reason: "pix_key_not_found", Status: "refund_failed"}))
	assert.Equal(t, []string{events.EventTypes.SendPush, events.EventTypes.SendEmail}, commandTypes(noPhone))
}

func TestRouterSkipsAndErrors(t *testing.T) {
	directory := tests.NewFakeDirectory(models.Contact{AccountID: ana.AccountID, UserID: ana.UserID, Name: "Ana"})
	router := newRouter(t, directory)

	noEmail := route(t, router, events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: ana.AccountID.String(), AccountType: "savings"}))
	assert.Empty(t, noEmail.Messages)

	assert.Empty(t, route(t, router, events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: "not-a-uuid"})).Messages)
	assert.Empty(t, route(t, router, events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: uuid.NewString()})).Messages)
	assert.Empty(t, route(t, router, events.NewAccountEvent(events.EventTypes.AccountCredited, events.AccountCreditedPayload{AccountID: ana.AccountID.String()})).Messages)
	calls := directory.Calls

	for _, eventType := range []string{events.EventTypes.AccountCreated, events.EventTypes.TransactionCompleted, events.EventTypes.TransactionFailed, events.EventTypes.TransferCompleted, events.EventTypes.TransferFailed, events.EventTypes.PaymentCompleted, events.EventTypes.PaymentFailed} {
		_, err := router.Dispatch(context.Background(), events.NewEvent(eventType, "x", "not an object"))
		assert.ErrorIs(t, err, processor.ErrBadPayload, eventType)
	}
	assert.Equal(t, calls, directory.Calls)

	directory.Err = errors.New("account service down")
	_, err := router.Dispatch(context.Background(), events.NewTransactionEvent(events.EventTypes.TransactionCompleted, events.TransactionCompletedPayload{AccountID: ana.AccountID.String()}))
	assert.EqualError(t, err, "account service down")
	_, err = router.Dispatch(context.Background(), events.NewTransactionEvent(events.EventTypes.TransferCompleted, events.TransferCompletedPayload{FromAccountID: ana.AccountID.String(), ToAccountID: bruno.AccountID.String()}))
	assert.EqualError(t, err, "account service down")
}

func TestRouterReportsTemplateErrors(t *testing.T) {
	broken, err := services.NewRenderer(fstest.MapFS{})
	assert.NoError(t, err)
	router := services.NewRouter(tests.NewFakeDirectory(ana), broken, services.SystemClock{}, time.Hour, logger.New(logger.Config{Output: &bytes.Buffer{}}))

	_, err = router.Dispatch(context.Background(), events.NewPaymentEvent(events.EventTypes.PaymentCompleted, events.PaymentCompletedPayload{AccountID: ana.AccountID.String(), PaymentMethod: "pix"}))
	assert.Equal(t, "template_error", domain.InvalidCode(err))
}

func TestRouterRendersLegacyNumericAmounts(t *testing.T) {
	router := newRouter(t, tests.NewFakeDirectory(ana))

	event, err := events.FromJSON([]byte(`{"type":"transaction.completed","payload":{"account_id":"` + ana.AccountID.String() + `","type":"deposit","amount":1234.56,"balance_after":1500}}`))
	assert.NoError(t, err)
	completed := route(t, router, event)

	assert.Equal(t, "Depósito de R$ 1.234,56 concluído. Saldo atual: R$ 1.500,00.", completed.Messages[0].Event.Payload.(events.SendPushPayload).Body)
}

func TestRouterRendersDecimalStringAmounts(t *testing.T) {
	router := newRouter(t, tests.NewFakeDirectory(ana))

	event, err := events.FromJSON([]byte(`{"type":"transaction.completed","payload":{"account_id":"` + ana.AccountID.String() + `","type":"deposit","amount":"0.10","balance_after":"1000000.00"}}`))
	assert.NoError(t, err)
	completed := route(t, router, event)

	assert.Equal(t, "Depósito de R$ 0,10 concluído. Saldo atual: R$ 1.000.000,00.", completed.Messages[0].Event.Payload.(events.SendPushPayload).Body)
}
