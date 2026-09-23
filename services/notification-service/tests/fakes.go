package tests

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
)

type FakeDirectory struct {
	Contacts map[uuid.UUID]models.Contact
	Err      error
	Calls    int
}

func NewFakeDirectory(contacts ...models.Contact) *FakeDirectory {
	d := &FakeDirectory{Contacts: map[uuid.UUID]models.Contact{}}
	for _, contact := range contacts {
		d.Contacts[contact.AccountID] = contact
	}
	return d
}

func (f *FakeDirectory) Lookup(_ context.Context, accountID uuid.UUID) (models.Contact, error) {
	f.Calls++
	if f.Err != nil {
		return models.Contact{}, f.Err
	}
	contact, ok := f.Contacts[accountID]
	if !ok {
		return models.Contact{}, domain.ErrNotFound
	}
	return contact, nil
}

type FakeSender struct {
	Sent []models.Message
	Errs []error
}

func (f *FakeSender) Send(_ context.Context, message models.Message) error {
	if len(f.Errs) > 0 {
		err := f.Errs[0]
		f.Errs = f.Errs[1:]
		if err != nil {
			return err
		}
	}
	f.Sent = append(f.Sent, message)
	return nil
}

type FakeHistory struct {
	Records []models.Record
	Err     error
}

func (f *FakeHistory) Append(_ context.Context, record models.Record) error {
	if f.Err != nil {
		return f.Err
	}
	f.Records = append([]models.Record{record}, f.Records...)
	return nil
}

func (f *FakeHistory) List(_ context.Context, userID uuid.UUID, limit int) ([]models.Record, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	result := []models.Record{}
	for _, record := range f.Records {
		if record.UserID == userID && len(result) < limit {
			result = append(result, record)
		}
	}
	return result, nil
}

type FakeStore struct {
	Seen map[uuid.UUID]bool
}

func NewFakeStore() *FakeStore {
	return &FakeStore{Seen: map[uuid.UUID]bool{}}
}

func (f *FakeStore) MarkProcessed(_ context.Context, eventID uuid.UUID) (bool, error) {
	first := !f.Seen[eventID]
	f.Seen[eventID] = true
	return first, nil
}

type PublishedEvent struct {
	Topic string
	Key   string
	Event *events.Event
}

type FakePublisher struct {
	Err       error
	Published []PublishedEvent
}

func (f *FakePublisher) Publish(_ context.Context, topic, key string, event *events.Event) error {
	if f.Err != nil {
		return f.Err
	}
	f.Published = append(f.Published, PublishedEvent{Topic: topic, Key: key, Event: event})
	return nil
}

func (f *FakePublisher) ByTopic(topic string) []PublishedEvent {
	result := []PublishedEvent{}
	for _, published := range f.Published {
		if published.Topic == topic {
			result = append(result, published)
		}
	}
	return result
}

type FakeClock struct {
	T time.Time
}

func (f FakeClock) Now() time.Time {
	return f.T
}
