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
	messages     []kafka.Message
	fetchErr     error
	commitErr    error
	committed    []kafka.Message
	commitCtxErr error
	closed       bool
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

func (f *fakeReader) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	f.commitCtxErr = ctx.Err()
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
	consumer := NewConsumerWithReader(reader, 0)
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
	consumer := NewConsumerWithReader(reader, 0)
	boom := errors.New("boom")

	err := consumer.Run(context.Background(), func(context.Context, kafka.Message) error { return boom })

	assert.ErrorIs(t, err, boom)
	assert.Empty(t, reader.committed)
}

func TestConsumerReturnsNilWhenContextCancelled(t *testing.T) {
	reader := &fakeReader{}
	consumer := NewConsumerWithReader(reader, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := consumer.Run(ctx, func(context.Context, kafka.Message) error { return nil })

	assert.NoError(t, err)
}

func TestConsumerSwallowsHandlerErrorsAfterCancel(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}}}
	consumer := NewConsumerWithReader(reader, 0)
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
	consumer := NewConsumerWithReader(reader, 0)

	err := consumer.Run(context.Background(), func(context.Context, kafka.Message) error { return nil })

	assert.ErrorContains(t, err, "broker gone")
}

func TestConsumerCloseClosesReader(t *testing.T) {
	reader := &fakeReader{}
	consumer := NewConsumerWithReader(reader, 0)

	assert.NoError(t, consumer.Close())
	assert.True(t, reader.closed)
}

func TestNewConsumerBuildsKafkaReader(t *testing.T) {
	consumer := NewConsumer(ConsumerConfig{Brokers: []string{"localhost:9092"}, GroupID: "g", Topic: "t"})

	assert.NotNil(t, consumer)
	assert.Equal(t, DefaultDrainTimeout, consumer.DrainTimeout())
	assert.NoError(t, consumer.Close())
}

func TestNewConsumerAppliesConfiguredDrainTimeout(t *testing.T) {
	consumer := NewConsumer(ConsumerConfig{Brokers: []string{"localhost:9092"}, GroupID: "g", Topic: "t", DrainTimeout: 5 * time.Second})

	assert.Equal(t, 5*time.Second, consumer.DrainTimeout())
	assert.NoError(t, consumer.Close())
}

func TestConsumerReturnsCommitErrors(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}}, commitErr: errors.New("commit failed")}
	consumer := NewConsumerWithReader(reader, 0)

	err := consumer.Run(context.Background(), func(context.Context, kafka.Message) error { return nil })

	assert.ErrorContains(t, err, "commit failed")
}

func TestConsumerFinishesInFlightMessageAfterCancel(t *testing.T) {
	reader := &fakeReader{messages: []kafka.Message{{Value: []byte("a")}, {Value: []byte("b")}}}
	consumer := NewConsumerWithReader(reader, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var handled int
	err := consumer.Run(ctx, func(handlerCtx context.Context, _ kafka.Message) error {
		handled++
		cancel()
		assert.NoError(t, handlerCtx.Err())
		deadline, ok := handlerCtx.Deadline()
		assert.True(t, ok)
		assert.WithinDuration(t, time.Now().Add(time.Minute), deadline, 5*time.Second)
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, handled)
	assert.Len(t, reader.committed, 1)
	assert.NoError(t, reader.commitCtxErr)
	assert.Len(t, reader.messages, 1)
}

func TestRunWithRestartRestartsFailedConsumers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readers := []*fakeReader{{fetchErr: errors.New("broker gone")}, {messages: []kafka.Message{{Value: []byte("a")}}}}
	var built int
	newConsumer := func() *Consumer {
		reader := readers[built]
		built++
		return NewConsumerWithReader(reader, 0)
	}

	var handled int
	var restarts []error
	RunWithRestart(ctx, newConsumer, func(context.Context, kafka.Message) error {
		handled++
		cancel()
		return nil
	}, []time.Duration{time.Millisecond}, func(err error) { restarts = append(restarts, err) })

	assert.Equal(t, 1, handled)
	assert.Len(t, restarts, 1)
	assert.ErrorContains(t, restarts[0], "broker gone")
	assert.True(t, readers[0].closed)
	assert.True(t, readers[1].closed)
	assert.Len(t, readers[1].committed, 1)
}

func TestRunWithRestartStopsDuringBackoffWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &fakeReader{fetchErr: errors.New("broker gone")}
	newConsumer := func() *Consumer { return NewConsumerWithReader(reader, 0) }

	var restarts int
	start := time.Now()
	RunWithRestart(ctx, newConsumer, func(context.Context, kafka.Message) error { return nil }, []time.Duration{time.Hour}, func(error) {
		restarts++
		time.AfterFunc(10*time.Millisecond, cancel)
	})

	assert.Equal(t, 1, restarts)
	assert.True(t, reader.closed)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestRunWithRestartReturnsWhenConsumerStopsCleanly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	reader := &fakeReader{}
	var restarts int

	RunWithRestart(ctx, func() *Consumer { return NewConsumerWithReader(reader, 0) }, func(context.Context, kafka.Message) error { return nil }, nil, func(error) { restarts++ })

	assert.Equal(t, 0, restarts)
	assert.True(t, reader.closed)
}

func TestRestartDelayClampsToLastBackoff(t *testing.T) {
	backoff := []time.Duration{time.Millisecond, time.Second}

	assert.Equal(t, time.Millisecond, restartDelay(backoff, 0))
	assert.Equal(t, time.Second, restartDelay(backoff, 1))
	assert.Equal(t, time.Second, restartDelay(backoff, 7))
	assert.Equal(t, time.Duration(0), restartDelay(nil, 3))
}
