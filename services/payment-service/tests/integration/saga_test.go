//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/gateway"
	appHttp "github.com/fintech-bank-platform/payment-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

const secret = "it-secret"

func brokers(t *testing.T) []string {
	raw := os.Getenv("KAFKA_BROKERS")
	if raw == "" {
		t.Skip("KAFKA_BROKERS not set")
	}
	return strings.Split(raw, ",")
}

func admin(t *testing.T, broker string) *kafka.Conn {
	conn, err := kafka.Dial("tcp", broker)
	require.NoError(t, err)
	defer conn.Close()
	controller, err := conn.Controller()
	require.NoError(t, err)
	ctrl, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	require.NoError(t, err)
	return ctrl
}

func createTopic(t *testing.T, broker, topic string) {
	ctrl := admin(t, broker)
	t.Cleanup(func() {
		_ = ctrl.DeleteTopics(topic)
		_ = ctrl.Close()
	})
	require.NoError(t, ctrl.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}))
}

func ensureTopic(t *testing.T, broker, topic string) {
	ctrl := admin(t, broker)
	defer ctrl.Close()
	err := ctrl.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1})
	if err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
		require.NoError(t, err)
	}
}

func newReader(t *testing.T, addrs []string, topic string) *kafka.Reader {
	t.Helper()
	return kafka.NewReader(kafka.ReaderConfig{Brokers: addrs, GroupID: groupAtTail(t, addrs, topic), Topic: topic, StartOffset: kafka.LastOffset, MinBytes: 1, MaxBytes: 1 << 20})
}

func awaitEvent(t *testing.T, ctx context.Context, reader *kafka.Reader, eventType, trace string, filters ...func(*events.Event) bool) *events.Event {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		msg, err := reader.ReadMessage(readCtx)
		cancel()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		event, err := events.FromJSON(msg.Value)
		if err == nil && event.Type == eventType && event.TraceID == trace && matches(event, filters) {
			return event
		}
	}
	t.Fatalf("event %s with trace %s not received", eventType, trace)
	return nil
}

func matches(event *events.Event, filters []func(*events.Event) bool) bool {
	for _, filter := range filters {
		if !filter(event) {
			return false
		}
	}
	return true
}

func withField(name, value string) func(*events.Event) bool {
	return func(event *events.Event) bool {
		return field(event, name) == value
	}
}

func storedIn(ctx context.Context, repo *database.PaymentRepository) func(*events.Event) bool {
	return func(event *events.Event) bool {
		id, err := uuid.Parse(field(event, "payment_id"))
		if err != nil {
			return false
		}
		_, err = repo.Get(ctx, id)
		return err == nil
	}
}

func field(event *events.Event, name string) string {
	value, _ := event.Payload.(map[string]interface{})[name].(string)
	return value
}

func answer(command *events.Event, kind string, balance int64) *events.Event {
	account, reference, key := field(command, "account_id"), field(command, "reference"), field(command, "idempotency_key")
	var payload interface{} = events.AccountDebitedPayload{AccountID: account, BalanceAfter: domain.AmountFromCents(balance), Reference: reference, IdempotencyKey: key}
	if kind == events.EventTypes.AccountCredited {
		payload = events.AccountCreditedPayload{AccountID: account, BalanceAfter: domain.AmountFromCents(balance), Reference: reference, IdempotencyKey: key}
	}
	return events.NewAccountEvent(kind, payload).WithTraceID(command.TraceID)
}

type providerCall struct {
	body      []byte
	timestamp string
	signature string
}

func TestPaymentsEndToEnd(t *testing.T) {
	addrs := brokers(t)
	session, _ := throwawayKeyspace(t)
	replies := "it.payment.replies." + uuid.NewString()
	createTopic(t, addrs[0], replies)
	for _, topic := range []string{events.Topics.PaymentCommands, events.Topics.PaymentEvents, events.Topics.PaymentDLQ, events.Topics.AccountCommands} {
		ensureTopic(t, addrs[0], topic)
	}

	producer := messaging.NewProducer(messaging.ProducerConfig{Brokers: addrs, WriteTimeout: 10 * time.Second, BatchTimeout: 10 * time.Millisecond, PublishTimeout: 20 * time.Second, MaxAttempts: 5})
	defer producer.Close()
	accountCommands := newReader(t, addrs, events.Topics.AccountCommands)
	defer accountCommands.Close()
	results := newReader(t, addrs, events.Topics.PaymentEvents)
	defer results.Close()

	calls := make(chan providerCall, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls <- providerCall{body: body, timestamp: r.Header.Get("X-Timestamp"), signature: r.Header.Get("X-Signature")}
	}))
	defer provider.Close()

	log := logger.New(logger.Config{Output: &bytes.Buffer{}})
	repo := database.NewPaymentRepository(session)
	simulator := gateway.NewSimulator(gateway.Config{WebhookURL: provider.URL, Secret: secret, Delay: 100 * time.Millisecond}, log)
	service := services.NewPaymentService(repo, simulator, services.SystemClock{}, uuid.New)
	store := database.NewProcessedEventStore(session)
	cfg := processor.Config{Source: "payment-service", FailedEventType: events.EventTypes.PaymentCommandFailed, DLQTopic: events.Topics.PaymentDLQ, Backoff: []time.Duration{100 * time.Millisecond}}
	commandProcessor := processor.NewProcessor(handlers.NewCommandDispatcher(service, log), store, producer, cfg, log)
	replyProcessor := processor.NewProcessor(handlers.NewReplyDispatcher(service, log), store, producer, cfg, log)

	router := chi.NewRouter()
	appHttp.SetupRouter(router, appHttp.Dependencies{
		Reads:    handlers.NewReadHandler(service),
		Webhooks: handlers.NewWebhookHandler(producer, secret, time.Minute, services.SystemClock{}, log),
		Ping:     database.Ping(session),
		Logger:   log,
	})
	api := httptest.NewServer(router)
	defer api.Close()

	prefix := "it-" + uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	commandConsumer := messaging.NewConsumer(messaging.ConsumerConfig{Brokers: addrs, GroupID: groupAtTail(t, addrs, events.Topics.PaymentCommands), Topic: events.Topics.PaymentCommands, StartOffset: kafka.LastOffset})
	replyConsumer := messaging.NewConsumer(messaging.ConsumerConfig{Brokers: addrs, GroupID: "it-replies-" + uuid.NewString(), Topic: replies})
	done := make(chan error, 2)
	go func() {
		done <- commandConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
			event, err := events.FromJSON(msg.Value)
			if err != nil || !strings.HasPrefix(event.TraceID, prefix) {
				return nil
			}
			return commandProcessor.Process(ctx, msg.Key, msg.Value)
		})
	}()
	go func() {
		done <- replyConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
			return replyProcessor.Process(ctx, msg.Key, msg.Value)
		})
	}()

	account := uuid.NewString()

	tedTrace := prefix + "-ted"
	ted := events.NewPaymentCommand(events.EventTypes.ProcessPayment, events.ProcessPaymentPayload{
		AccountID: account, PaymentMethod: "ted", Amount: domain.AmountFromCents(100000), Currency: "BRL", Recipient: "Bruno Lima", IdempotencyKey: tedTrace,
		TED: &events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"},
	}).WithTraceID(tedTrace)
	require.NoError(t, producer.Publish(ctx, events.Topics.PaymentCommands, account, ted))

	created := awaitEvent(t, ctx, results, events.EventTypes.PaymentCreated, tedTrace, storedIn(ctx, repo))
	tedID := uuid.MustParse(field(created, "payment_id"))
	debit := awaitEvent(t, ctx, accountCommands, events.EventTypes.DebitAccount, tedTrace, withField("reference", models.Reference(tedID)))
	require.Equal(t, models.StepKey(tedID, models.StepDebit), field(debit, "idempotency_key"))
	require.NoError(t, producer.Publish(ctx, replies, account, answer(debit, events.EventTypes.AccountDebited, 900000)))

	processed := awaitEvent(t, ctx, results, events.EventTypes.PaymentProcessed, tedTrace, withField("payment_id", tedID.String()))
	externalID := field(processed, "external_id")
	require.True(t, strings.HasPrefix(externalID, "ted_"))

	var call providerCall
	select {
	case call = <-calls:
	case <-ctx.Done():
		t.Fatal("provider callback not received")
	}
	require.NoError(t, services.Verify(secret, call.timestamp, call.body, call.signature, time.Now(), time.Minute))
	require.Contains(t, string(call.body), externalID)

	forward, err := http.NewRequest(http.MethodPost, api.URL+"/webhooks/gateway", bytes.NewReader(call.body))
	require.NoError(t, err)
	forward.Header.Set("X-Timestamp", call.timestamp)
	forward.Header.Set("X-Signature", call.signature)
	forward.Header.Set("X-Request-ID", tedTrace+"-settle")
	resp, err := http.DefaultClient.Do(forward)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	completed := awaitEvent(t, ctx, results, events.EventTypes.PaymentCompleted, tedTrace+"-settle", withField("payment_id", tedID.String()))
	require.Equal(t, externalID, field(completed, "external_id"))
	stored, err := repo.Get(ctx, tedID)
	require.NoError(t, err)
	require.Equal(t, models.StatusCompleted, stored.Status)
	require.Equal(t, externalID, stored.ExternalID)
	require.Equal(t, int64(900000), *stored.BalanceAfterCents)

	read, err := http.Get(api.URL + "/payments/" + tedID.String())
	require.NoError(t, err)
	readBody, _ := io.ReadAll(read.Body)
	read.Body.Close()
	require.Equal(t, http.StatusOK, read.StatusCode)
	require.Contains(t, string(readBody), `"status":"completed"`)

	pixTrace := prefix + "-pix"
	pix := events.NewPaymentCommand(events.EventTypes.ProcessPayment, events.ProcessPaymentPayload{
		AccountID: account, PaymentMethod: "pix", Amount: domain.AmountFromCents(2000), Currency: "BRL", Recipient: "Ana", PixKey: "reject@reject.test", IdempotencyKey: pixTrace,
	}).WithTraceID(pixTrace)
	require.NoError(t, producer.Publish(ctx, events.Topics.PaymentCommands, account, pix))
	pixID := uuid.MustParse(field(awaitEvent(t, ctx, results, events.EventTypes.PaymentCreated, pixTrace, storedIn(ctx, repo)), "payment_id"))
	pixDebit := awaitEvent(t, ctx, accountCommands, events.EventTypes.DebitAccount, pixTrace, withField("reference", models.Reference(pixID)))
	require.Equal(t, models.StepKey(pixID, models.StepDebit), field(pixDebit, "idempotency_key"))
	require.NoError(t, producer.Publish(ctx, replies, account, answer(pixDebit, events.EventTypes.AccountDebited, 898000)))
	refund := awaitEvent(t, ctx, accountCommands, events.EventTypes.CreditAccount, pixTrace, withField("reference", models.Reference(pixID)))
	require.Equal(t, models.StepKey(pixID, models.StepRefund), field(refund, "idempotency_key"))
	require.NoError(t, producer.Publish(ctx, replies, account, answer(refund, events.EventTypes.AccountCredited, 900000)))

	failed := awaitEvent(t, ctx, results, events.EventTypes.PaymentFailed, pixTrace, withField("payment_id", pixID.String()))
	require.Equal(t, "refunded", field(failed, "status"))
	require.Equal(t, "pix_key_not_found", field(failed, "reason"))

	page, err := repo.ListByAccount(ctx, uuid.MustParse(account), nil, 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)

	cancel()
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.NoError(t, commandConsumer.Close())
	require.NoError(t, replyConsumer.Close())
}
