package tests

import (
	"context"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/segmentio/kafka-go"
)

type PublishedCommand struct {
	Topic string
	Key   string
	Event *events.Event
}

type FakePublisher struct {
	Err       error
	Published []PublishedCommand
}

func (f *FakePublisher) Publish(_ context.Context, topic, key string, event *events.Event) error {
	if f.Err != nil {
		return f.Err
	}
	f.Published = append(f.Published, PublishedCommand{Topic: topic, Key: key, Event: event})
	return nil
}

func (f *FakePublisher) Last() PublishedCommand {
	return f.Published[len(f.Published)-1]
}

type FakeWriter struct {
	Err      error
	Messages []kafka.Message
	Closed   bool
}

func (f *FakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	if f.Err != nil {
		return f.Err
	}
	f.Messages = append(f.Messages, msgs...)
	return nil
}

func (f *FakeWriter) Close() error {
	f.Closed = true
	return nil
}
