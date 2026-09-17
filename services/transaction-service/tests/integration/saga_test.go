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

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/handlers"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
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

func newReader(addrs []string, topic string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{Brokers: addrs, GroupID: "it-" + uuid.NewString(), Topic: topic, StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 1 << 20})
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

func accountReply(command *events.Event, kind string, balance float64, reason string) *events.Event {
	payload := command.Payload.(map[string]interface{})
	account, reference, key := payload["account_id"].(string), payload["reference"].(string), payload["idempotency_key"].(string)
	switch kind {
	case events.EventTypes.AccountDebited:
		return events.NewAccountEvent(kind, events.AccountDebitedPayload{AccountID: account, BalanceAfter: balance, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
	case events.EventTypes.AccountCredited:
		return events.NewAccountEvent(kind, events.AccountCreditedPayload{AccountID: account, BalanceAfter: balance, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
	case events.EventTypes.DebitRejected:
		return events.NewAccountEvent(kind, events.DebitRejectedPayload{AccountID: account, Reason: reason, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
	}
	return events.NewAccountEvent(kind, events.CreditRejectedPayload{AccountID: account, Reason: reason, Reference: reference, IdempotencyKey: key}).WithTraceID(command.TraceID)
}

func TestTransferSagaEndToEnd(t *testing.T) {
	addrs := brokers(t)
	session, _ := throwawayKeyspace(t)
	commands := "it.tx.commands." + uuid.NewString()
	replies := "it.account.events." + uuid.NewString()
	createTopic(t, addrs[0], commands)
	createTopic(t, addrs[0], replies)
	for _, topic := range []string{events.Topics.AccountCommands, events.Topics.TransactionEvents, events.Topics.TransactionDLQ} {
		ensureTopic(t, addrs[0], topic)
	}

	producer := messaging.NewProducer(messaging.ProducerConfig{Brokers: addrs, WriteTimeout: 10 * time.Second, BatchTimeout: 10 * time.Millisecond, PublishTimeout: 20 * time.Second, MaxAttempts: 5})
	defer producer.Close()
	accountCommands := newReader(addrs, events.Topics.AccountCommands)
	defer accountCommands.Close()
	results := newReader(addrs, events.Topics.TransactionEvents)
	defer results.Close()

	repo := database.NewTransactionRepository(session)
	service := services.NewTransactionService(repo, services.SystemClock{}, uuid.New)
	store := database.NewProcessedEventStore(session)
	log := logger.New(logger.Config{Output: &bytes.Buffer{}})
	cfg := processor.Config{Source: "transaction-service", FailedEventType: events.EventTypes.TransactionCommandFailed, DLQTopic: events.Topics.TransactionDLQ, Backoff: []time.Duration{100 * time.Millisecond}}
	commandProcessor := processor.NewProcessor(handlers.NewCommandDispatcher(service, log), store, producer, cfg, log)
	replyProcessor := processor.NewProcessor(handlers.NewReplyDispatcher(service, log), store, producer, cfg, log)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	commandConsumer := messaging.NewConsumer(messaging.ConsumerConfig{Brokers: addrs, GroupID: "it-commands-" + uuid.NewString(), Topic: commands})
	replyConsumer := messaging.NewConsumer(messaging.ConsumerConfig{Brokers: addrs, GroupID: "it-replies-" + uuid.NewString(), Topic: replies})
	done := make(chan error, 2)
	go func() {
		done <- commandConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
			return commandProcessor.Process(ctx, msg.Key, msg.Value)
		})
	}()
	go func() {
		done <- replyConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
			return replyProcessor.Process(ctx, msg.Key, msg.Value)
		})
	}()

	from, to := uuid.NewString(), uuid.NewString()
	trace := "it-" + uuid.NewString()
	transfer := events.NewTransactionCommand(events.EventTypes.ProcessTransfer, events.ProcessTransferPayload{FromAccountID: from, ToAccountID: to, Amount: 30, Currency: "BRL", IdempotencyKey: "tr-" + trace}).WithTraceID(trace)
	require.NoError(t, producer.Publish(ctx, commands, from, transfer))

	created := awaitEvent(t, ctx, results, events.EventTypes.TransactionCreated, trace)
	transferID := uuid.MustParse(created.Payload.(map[string]interface{})["transaction_id"].(string))

	debit := awaitEvent(t, ctx, accountCommands, events.EventTypes.DebitAccount, trace)
	require.Equal(t, from, debit.Payload.(map[string]interface{})["account_id"])
	require.NoError(t, producer.Publish(ctx, replies, from, accountReply(debit, events.EventTypes.AccountDebited, 70, "")))

	credit := awaitEvent(t, ctx, accountCommands, events.EventTypes.CreditAccount, trace)
	require.Equal(t, to, credit.Payload.(map[string]interface{})["account_id"])
	require.Equal(t, models.StepKey(transferID, models.StepCredit), credit.Payload.(map[string]interface{})["idempotency_key"])
	require.NoError(t, producer.Publish(ctx, replies, to, accountReply(credit, events.EventTypes.AccountCredited, 30, "")))

	completed := awaitEvent(t, ctx, results, events.EventTypes.TransferCompleted, trace)
	payload := completed.Payload.(map[string]interface{})
	require.Equal(t, 70.0, payload["from_balance_after"])
	require.Equal(t, 30.0, payload["to_balance_after"])

	stored, err := repo.Get(ctx, transferID)
	require.NoError(t, err)
	require.Equal(t, models.StatusCompleted, stored.Status)
	require.Equal(t, int64(7000), *stored.FromBalanceCents)
	require.Equal(t, int64(3000), *stored.ToBalanceCents)

	require.NoError(t, producer.Publish(ctx, commands, from, transfer))
	deposit := events.NewTransactionCommand(events.EventTypes.CreateTransaction, events.CreateTransactionPayload{AccountID: from, Type: "deposit", Amount: 5, Currency: "BRL", IdempotencyKey: "dep-" + trace}).WithTraceID(trace + "-deposit")
	require.NoError(t, producer.Publish(ctx, commands, from, deposit))
	awaitEvent(t, ctx, results, events.EventTypes.TransactionCreated, trace+"-deposit")

	list, err := repo.ListByAccount(ctx, uuid.MustParse(from), 10)
	require.NoError(t, err)
	require.Len(t, list, 2)

	cancel()
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.NoError(t, commandConsumer.Close())
	require.NoError(t, replyConsumer.Close())
}
