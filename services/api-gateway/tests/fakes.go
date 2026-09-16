package tests

import (
	"context"

	"github.com/fintech-bank-platform/pkg/events"
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
