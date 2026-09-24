//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
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

func createTopic(t *testing.T, broker, topic string) {
	conn, err := kafka.Dial("tcp", broker)
	require.NoError(t, err)
	defer conn.Close()
	controller, err := conn.Controller()
	require.NoError(t, err)
	admin, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = admin.DeleteTopics(topic)
		_ = admin.Close()
	})
	require.NoError(t, admin.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}))
}

func ensureTopic(t *testing.T, broker, topic string) {
	conn, err := kafka.Dial("tcp", broker)
	require.NoError(t, err)
	defer conn.Close()
	controller, err := conn.Controller()
	require.NoError(t, err)
	admin, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	require.NoError(t, err)
	defer admin.Close()
	err = admin.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1})
	if err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
		require.NoError(t, err)
	}
}

func TestConsumerAppliesCommandsEndToEnd(t *testing.T) {
	addrs := brokers(t)
	session, _ := throwawayKeyspace(t)
	commands := "it.commands." + uuid.NewString()
	createTopic(t, addrs[0], commands)
	ensureTopic(t, addrs[0], events.Topics.AccountEvents)
	ensureTopic(t, addrs[0], events.Topics.AccountDLQ)

	producer := messaging.NewProducer(messaging.ProducerConfig{Brokers: addrs, WriteTimeout: 10 * time.Second, BatchTimeout: 10 * time.Millisecond, PublishTimeout: 20 * time.Second, MaxAttempts: 5})
	defer producer.Close()

	results := kafka.NewReader(kafka.ReaderConfig{Brokers: addrs, GroupID: groupAtTail(t, addrs, events.Topics.AccountEvents), Topic: events.Topics.AccountEvents, StartOffset: kafka.LastOffset, MinBytes: 1, MaxBytes: 1 << 20})
	defer results.Close()

	service := services.NewAccountService(database.NewAccountRepository(session), database.NewCustomerRepository(session), database.NewBalanceOperationRepository(session), services.SystemClock{}, services.RandomNumber)
	proc := processor.NewProcessor(handlers.NewDispatcher(service), database.NewProcessedEventStore(session), producer, processor.Config{
		Source:          "account-service",
		FailedEventType: events.EventTypes.AccountCommandFailed,
		DLQTopic:        events.Topics.AccountDLQ,
		Backoff:         []time.Duration{100 * time.Millisecond},
	}, logger.New(logger.Config{Output: &bytes.Buffer{}}))
	consumer := messaging.NewConsumer(messaging.ConsumerConfig{Brokers: addrs, GroupID: "it-consumer-" + uuid.NewString(), Topic: commands})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- consumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error { return proc.Process(ctx, msg.Key, msg.Value) })
	}()

	trace := "it-" + uuid.NewString()
	create := events.NewAccountCommand(events.EventTypes.CreateAccount, events.CreateAccountPayload{UserID: uuid.NewString(), AccountType: "checking", Name: "Carla Dias", Email: "carla@example.com", Document: "52998224725"}).WithTraceID(trace)
	require.NoError(t, producer.Publish(ctx, commands, "u", create))
	require.NoError(t, producer.Publish(ctx, commands, "u", create))

	created := awaitEvent(t, ctx, results, events.EventTypes.AccountCreated, trace)
	payload := created.Payload.(map[string]interface{})
	accountID := uuid.MustParse(payload["account_id"].(string))
	account, err := database.NewAccountRepository(session).Get(ctx, accountID)
	require.NoError(t, err)
	require.Equal(t, "active", string(account.Status))

	credit := events.NewAccountCommand(events.EventTypes.CreditAccount, events.CreditAccountPayload{AccountID: accountID.String(), Amount: 12.5, Currency: "BRL", Reference: "tx-1", IdempotencyKey: "k-1"}).WithTraceID(trace + "-credit")
	require.NoError(t, producer.Publish(ctx, commands, accountID.String(), credit))
	credited := awaitEvent(t, ctx, results, events.EventTypes.AccountCredited, trace+"-credit")
	require.Equal(t, 12.5, credited.Payload.(map[string]interface{})["balance_after"])

	account, _ = database.NewAccountRepository(session).Get(ctx, accountID)
	require.Equal(t, int64(1250), account.BalanceCents)

	list, err := database.NewAccountRepository(session).ListByUser(ctx, account.UserID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	cancel()
	require.NoError(t, <-done)
	require.NoError(t, consumer.Close())
}

func awaitEvent(t *testing.T, ctx context.Context, reader *kafka.Reader, eventType, trace string) *events.Event {
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
		if err == nil && event.Type == eventType && event.TraceID == trace {
			return event
		}
	}
	t.Fatalf("event %s with trace %s not received", eventType, trace)
	return nil
}
