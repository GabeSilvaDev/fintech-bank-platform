package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
)

type fakeReader struct {
	messages  []kafka.Message
	fetchErr  error
	commitErr error
	committed []kafka.Message
	closed    bool
}

func (f *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if f.fetchErr != nil {
		return kafka.Message{}, f.fetchErr
	}
	if len(f.messages) == 0 {
		<-ctx.Done()
		return kafka.Message{}, ctx.Err()
	}
	msg := f.messages[0]
	f.messages = f.messages[1:]
	return msg, nil
}

func (f *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = append(f.committed, msgs...)
	return nil
}

func (f *fakeReader) Close() error {
	f.closed = true
	return nil
}

func TestConsumerHandlesAndCommitsEachMessage(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}, {Value: []byte("b")}}}
	consumer := NewConsumerWithReader(reader)
	ctx, cancel := context.WithCancel(context.Background())

	var handled []string
	err := consumer.Run(ctx, func(_ context.Context, msg kafka.Message) error {
		handled = append(handled, string(msg.Value))
		if len(handled) == 2 {
			cancel()
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, handled)
	assert.Len(t, reader.committed, 2)
}

func TestConsumerStopsOnHandlerError(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}}}
	consumer := NewConsumerWithReader(reader)
	boom := errors.New("boom")

	err := consumer.Run(context.Background(), func(context.Context, kafka.Message) error { return boom })

	assert.ErrorIs(t, err, boom)
	assert.Empty(t, reader.committed)
}

func TestConsumerReturnsNilWhenContextCancelled(t *testing.T) {
	reader := &fakeReader{}
	consumer := NewConsumerWithReader(reader)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := consumer.Run(ctx, func(context.Context, kafka.Message) error { return nil })

	assert.NoError(t, err)
}

func TestConsumerSwallowsHandlerErrorsAfterCancel(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}}}
	consumer := NewConsumerWithReader(reader)
	ctx, cancel := context.WithCancel(context.Background())

	err := consumer.Run(ctx, func(context.Context, kafka.Message) error {
		cancel()
		return context.Canceled
	})

	assert.NoError(t, err)
	assert.Empty(t, reader.committed)
}

func TestConsumerReturnsFetchErrors(t *testing.T) {
	reader := &fakeReader{fetchErr: errors.New("broker gone")}
	consumer := NewConsumerWithReader(reader)

	err := consumer.Run(context.Background(), func(context.Context, kafka.Message) error { return nil })

	assert.ErrorContains(t, err, "broker gone")
}

func TestConsumerCloseClosesReader(t *testing.T) {
	reader := &fakeReader{}
	consumer := NewConsumerWithReader(reader)

	assert.NoError(t, consumer.Close())
	assert.True(t, reader.closed)
}

func TestNewConsumerBuildsKafkaReader(t *testing.T) {
	consumer := NewConsumer(ConsumerConfig{Brokers: []string{"localhost:9092"}, GroupID: "g", Topic: "t"})

	assert.NotNil(t, consumer)
	assert.NoError(t, consumer.Close())
}

func TestConsumerReturnsCommitErrors(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}}, commitErr: errors.New("commit failed")}
	consumer := NewConsumerWithReader(reader)

	err := consumer.Run(context.Background(), func(context.Context, kafka.Message) error { return nil })

	assert.ErrorContains(t, err, "commit failed")
}
