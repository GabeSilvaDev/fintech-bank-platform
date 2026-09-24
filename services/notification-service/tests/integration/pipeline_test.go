//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/app/services"
	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/directory"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/storage"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

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

func ensureTopic(t *testing.T, broker, topic string) {
	ctrl := admin(t, broker)
	defer ctrl.Close()
	err := ctrl.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1})
	if err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
		require.NoError(t, err)
	}
}

type recordingSender struct {
	mu   sync.Mutex
	sent []models.Message
	ch   chan models.Message
}

func newRecordingSender() *recordingSender {
	return &recordingSender{ch: make(chan models.Message, 32)}
}

func (r *recordingSender) Send(_ context.Context, message models.Message) error {
	r.mu.Lock()
	r.sent = append(r.sent, message)
	r.mu.Unlock()
	r.ch <- message
	return nil
}

func awaitMessages(t *testing.T, ch <-chan models.Message, count int, timeout time.Duration) []models.Message {
	t.Helper()
	deadline := time.After(timeout)
	messages := make([]models.Message, 0, count)
	for len(messages) < count {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-deadline:
			t.Fatalf("timed out waiting for %d messages, got %d: %+v", count, len(messages), messages)
		}
	}
	return messages
}

func ownerHandler(contacts map[string]models.Contact) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "accounts" || parts[2] != "owner" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		contact, ok := contacts[parts[1]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{
				"account_id": contact.AccountID.String(),
				"user_id":    contact.UserID.String(),
				"name":       contact.Name,
				"email":      contact.Email,
				"phone":      contact.Phone,
			},
		})
	}
}

func filteredHandle(prefix string, proc *processor.Processor) messaging.Handler {
	return func(ctx context.Context, msg kafka.Message) error {
		event, err := events.FromJSON(msg.Value)
		if err != nil || !strings.HasPrefix(event.TraceID, prefix) {
			return nil
		}
		return proc.Process(ctx, msg.Key, msg.Value)
	}
}

func runFilteredConsumer(ctx context.Context, addrs []string, topic, groupID string, handle messaging.Handler, done chan<- error) *messaging.Consumer {
	consumer := messaging.NewConsumer(messaging.ConsumerConfig{Brokers: addrs, GroupID: groupID, Topic: topic, StartOffset: kafka.LastOffset})
	go func() {
		done <- consumer.Run(ctx, handle)
	}()
	return consumer
}

func TestNotificationPipelineEndToEnd(t *testing.T) {
	addrs := brokers(t)
	client := redisClient(t)

	for _, topic := range []string{events.Topics.AccountEvents, events.Topics.TransactionEvents, events.Topics.PaymentEvents, events.Topics.NotificationEvents, events.Topics.NotificationDLQ} {
		ensureTopic(t, addrs[0], topic)
	}

	ana := models.Contact{AccountID: uuid.New(), UserID: uuid.New(), Name: "Ana Souza", Email: "ana@example.com", Phone: "+5511999887766"}
	bruno := models.Contact{AccountID: uuid.New(), UserID: uuid.New(), Name: "Bruno Lima", Email: "bruno@example.com"}

	directorySrv := httptest.NewServer(ownerHandler(map[string]models.Contact{
		ana.AccountID.String():   ana,
		bruno.AccountID.String(): bruno,
	}))
	defer directorySrv.Close()

	renderer, err := services.NewRenderer(services.Templates())
	require.NoError(t, err)
	dir := directory.NewClient(directorySrv.URL, 3*time.Second, time.Minute)
	router := services.NewRouter(dir, renderer, services.SystemClock{}, time.Hour, logger.New(logger.Config{Output: &bytes.Buffer{}}))

	emailSender := newRecordingSender()
	smsSender := newRecordingSender()
	pushSender := newRecordingSender()
	senderMap := map[models.Channel]contracts.Sender{
		models.ChannelEmail: emailSender,
		models.ChannelSMS:   smsSender,
		models.ChannelPush:  pushSender,
	}
	history := storage.NewHistory(client, 100)
	delivery := services.NewDelivery(senderMap, history, services.SystemClock{}, logger.New(logger.Config{Output: &bytes.Buffer{}}))

	producer := messaging.NewProducer(messaging.ProducerConfig{Brokers: addrs, WriteTimeout: 10 * time.Second, BatchTimeout: 10 * time.Millisecond, PublishTimeout: 20 * time.Second, MaxAttempts: 5})
	defer producer.Close()

	log := logger.New(logger.Config{Output: &bytes.Buffer{}})
	store := storage.NewStore(client)
	cfg := processor.Config{Source: "notification-service", FailedEventType: events.EventTypes.NotificationCommandFailed, DLQTopic: events.Topics.NotificationDLQ, Backoff: []time.Duration{100 * time.Millisecond}}
	routingProcessor := processor.NewProcessor(router, store, producer, cfg, log)
	deliveryProcessor := processor.NewProcessor(delivery, store, producer, cfg, log)

	prefix := "it-notif-" + uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	done := make(chan error, 4)
	accountsConsumer := runFilteredConsumer(ctx, addrs, events.Topics.AccountEvents, groupAtTail(t, addrs, events.Topics.AccountEvents), filteredHandle(prefix, routingProcessor), done)
	transactionsConsumer := runFilteredConsumer(ctx, addrs, events.Topics.TransactionEvents, groupAtTail(t, addrs, events.Topics.TransactionEvents), filteredHandle(prefix, routingProcessor), done)
	paymentsConsumer := runFilteredConsumer(ctx, addrs, events.Topics.PaymentEvents, groupAtTail(t, addrs, events.Topics.PaymentEvents), filteredHandle(prefix, routingProcessor), done)
	deliveryConsumer := runFilteredConsumer(ctx, addrs, events.Topics.NotificationEvents, groupAtTail(t, addrs, events.Topics.NotificationEvents), filteredHandle(prefix, deliveryProcessor), done)

	accountTrace := prefix + "-account"
	accountCreated := events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{
		AccountID: ana.AccountID.String(), UserID: ana.UserID.String(), AccountNumber: "12345678", Agency: "0001", AccountType: "checking",
	}).WithTraceID(accountTrace)
	require.NoError(t, producer.Publish(ctx, events.Topics.AccountEvents, ana.AccountID.String(), accountCreated))

	welcome := awaitMessages(t, emailSender.ch, 1, 60*time.Second)
	require.Equal(t, ana.Email, welcome[0].To)
	require.Equal(t, "Bem-vindo(a) ao Fintech Bank", welcome[0].Subject)

	transferTrace := prefix + "-transfer"
	transferCompleted := events.NewTransactionEvent(events.EventTypes.TransferCompleted, events.TransferCompletedPayload{
		FromAccountID: ana.AccountID.String(), ToAccountID: bruno.AccountID.String(), Amount: domain.AmountFromCents(3000), FromBalanceAfter: domain.AmountFromCents(7000), ToBalanceAfter: domain.AmountFromCents(3000),
	}).WithTraceID(transferTrace)
	require.NoError(t, producer.Publish(ctx, events.Topics.TransactionEvents, ana.AccountID.String(), transferCompleted))

	transferPushes := awaitMessages(t, pushSender.ch, 2, 60*time.Second)
	recipients := []string{transferPushes[0].To, transferPushes[1].To}
	require.ElementsMatch(t, []string{ana.UserID.String(), bruno.UserID.String()}, recipients)

	paymentTrace := prefix + "-payment"
	paymentFailed := events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{
		AccountID: ana.AccountID.String(), PaymentMethod: "pix", Amount: domain.AmountFromCents(1000), Reason: "pix_key_not_found", Status: "refund_failed",
	}).WithTraceID(paymentTrace)
	require.NoError(t, producer.Publish(ctx, events.Topics.PaymentEvents, ana.AccountID.String(), paymentFailed))

	paymentPush := awaitMessages(t, pushSender.ch, 1, 60*time.Second)
	require.Equal(t, ana.UserID.String(), paymentPush[0].To)
	paymentEmail := awaitMessages(t, emailSender.ch, 1, 60*time.Second)
	require.Equal(t, ana.Email, paymentEmail[0].To)
	paymentSMS := awaitMessages(t, smsSender.ch, 1, 60*time.Second)
	require.Equal(t, ana.Phone, paymentSMS[0].To)

	deadline := time.Now().Add(10 * time.Second)
	var records []models.Record
	for time.Now().Before(deadline) {
		records, err = history.List(ctx, ana.UserID, 20)
		require.NoError(t, err)
		if len(records) >= 4 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.GreaterOrEqual(t, len(records), 4)
	for i := 1; i < len(records); i++ {
		require.False(t, records[i-1].SentAt.Before(records[i].SentAt))
	}

	cancel()
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.NoError(t, accountsConsumer.Close())
	require.NoError(t, transactionsConsumer.Close())
	require.NoError(t, paymentsConsumer.Close())
	require.NoError(t, deliveryConsumer.Close())
}
