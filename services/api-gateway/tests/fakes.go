package tests

import (
	"context"
	"sync"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
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

type FakeOwners struct {
	mu       sync.Mutex
	owners   map[uuid.UUID]uuid.UUID
	fallback uuid.UUID
	Err      error
	Lookups  []uuid.UUID
}

func NewFakeOwners() *FakeOwners {
	return &FakeOwners{owners: make(map[uuid.UUID]uuid.UUID)}
}

func OwnedBy(userID uuid.UUID) *FakeOwners {
	owners := NewFakeOwners()
	owners.fallback = userID
	return owners
}

func (f *FakeOwners) Own(accountID string, userID uuid.UUID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[uuid.MustParse(accountID)] = userID
	return accountID
}

func (f *FakeOwners) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = make(map[uuid.UUID]uuid.UUID)
	f.Err = nil
	f.Lookups = nil
}

func (f *FakeOwners) Owner(_ context.Context, accountID uuid.UUID) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Lookups = append(f.Lookups, accountID)
	if f.Err != nil {
		return uuid.Nil, f.Err
	}
	if owner, ok := f.owners[accountID]; ok {
		return owner, nil
	}
	if f.fallback != uuid.Nil {
		return f.fallback, nil
	}
	return uuid.Nil, contracts.ErrAccountNotFound
}
