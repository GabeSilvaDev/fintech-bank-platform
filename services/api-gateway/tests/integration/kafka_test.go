//go:build integration

package integration

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/messaging"
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

func TestProducerRoundTripsThroughKafka(t *testing.T) {
	addrs := brokers(t)
	topic := "it." + uuid.NewString()
	createTopic(t, addrs[0], topic)

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        addrs,
		WriteTimeout:   10 * time.Second,
		BatchTimeout:   10 * time.Millisecond,
		PublishTimeout: 20 * time.Second,
		MaxAttempts:    5,
	})
	defer producer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sent := events.NewAccountCommand(events.EventTypes.CreateAccount, events.CreateAccountPayload{UserID: "u1"}).WithTraceID("trace-it")
	require.NoError(t, producer.Publish(ctx, topic, "u1", sent))

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   addrs,
		Topic:     topic,
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer reader.Close()
	require.NoError(t, reader.SetOffset(kafka.FirstOffset))

	msg, err := reader.ReadMessage(ctx)
	require.NoError(t, err)

	got, err := events.FromJSON(msg.Value)
	require.NoError(t, err)
	require.Equal(t, sent.ID, got.ID)
	require.Equal(t, "trace-it", got.TraceID)
	require.Equal(t, "u1", string(msg.Key))
	require.Equal(t, []kafka.Header{
		{Key: "event_type", Value: []byte(events.EventTypes.CreateAccount)},
		{Key: "trace_id", Value: []byte("trace-it")},
	}, msg.Headers)
}
