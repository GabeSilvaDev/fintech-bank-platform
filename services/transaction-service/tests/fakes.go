package tests

import (
	"context"
	"sort"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/google/uuid"
)

type TransitionCall struct {
	ID    uuid.UUID
	From  models.TransactionStatus
	To    models.TransactionStatus
	Patch models.Patch
}

type TransitionResult struct {
	Applied bool
	Err     error
}

type TouchCall struct {
	ID       uuid.UUID
	Status   models.TransactionStatus
	Observed time.Time
	Now      time.Time
}

type FakeTransactionRepo struct {
	Transactions      map[uuid.UUID]*models.Transaction
	Keys              map[string]uuid.UUID
	Created           []*models.Transaction
	Transitions       []TransitionCall
	TransitionResults []TransitionResult
	Touches           []TouchCall
	TouchResults      []TransitionResult
	GetErrs           []error
	Err               error
	CreateErr         error
	ReserveErr        error
	OnTransition      func()
	StaleErr          error
	StaleMaxAges      []time.Duration
}

func NewFakeTransactionRepo() *FakeTransactionRepo {
	return &FakeTransactionRepo{Transactions: map[uuid.UUID]*models.Transaction{}, Keys: map[string]uuid.UUID{}}
}

func (f *FakeTransactionRepo) Put(tx *models.Transaction) {
	copied := *tx
	f.Transactions[tx.ID] = &copied
}

func (f *FakeTransactionRepo) Create(_ context.Context, tx *models.Transaction) error {
	if f.CreateErr != nil {
		return f.CreateErr
	}
	if f.Err != nil {
		return f.Err
	}
	f.Put(tx)
	f.Created = append(f.Created, tx)
	return nil
}

func (f *FakeTransactionRepo) ReserveKey(_ context.Context, key string, id uuid.UUID) (uuid.UUID, error) {
	if f.ReserveErr != nil {
		return uuid.Nil, f.ReserveErr
	}
	if f.Err != nil {
		return uuid.Nil, f.Err
	}
	if owner, taken := f.Keys[key]; taken {
		return owner, nil
	}
	f.Keys[key] = id
	return id, nil
}

func (f *FakeTransactionRepo) Get(_ context.Context, id uuid.UUID) (*models.Transaction, error) {
	if len(f.GetErrs) > 0 {
		err := f.GetErrs[0]
		f.GetErrs = f.GetErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	if f.Err != nil {
		return nil, f.Err
	}
	tx, ok := f.Transactions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *tx
	return &copied, nil
}

func (f *FakeTransactionRepo) ListByAccount(_ context.Context, accountID uuid.UUID, limit int) ([]*models.Transaction, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	result := []*models.Transaction{}
	for _, tx := range f.Transactions {
		if tx.AccountID == accountID || (tx.CounterpartyID != nil && *tx.CounterpartyID == accountID) {
			copied := *tx
			result = append(result, &copied)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (f *FakeTransactionRepo) Transition(_ context.Context, id uuid.UUID, from, to models.TransactionStatus, patch models.Patch) (bool, error) {
	if f.OnTransition != nil {
		f.OnTransition()
	}
	f.Transitions = append(f.Transitions, TransitionCall{ID: id, From: from, To: to, Patch: patch})
	if len(f.TransitionResults) > 0 {
		result := f.TransitionResults[0]
		f.TransitionResults = f.TransitionResults[1:]
		return result.Applied, result.Err
	}
	if f.Err != nil {
		return false, f.Err
	}
	tx, ok := f.Transactions[id]
	if !ok || tx.Status != from {
		return false, nil
	}
	tx.Status = to
	tx.UpdatedAt = patch.UpdatedAt
	if patch.FailureReason != nil {
		tx.FailureReason = *patch.FailureReason
	}
	if patch.FromBalanceCents != nil {
		tx.FromBalanceCents = patch.FromBalanceCents
	}
	if patch.ToBalanceCents != nil {
		tx.ToBalanceCents = patch.ToBalanceCents
	}
	if patch.CompletedAt != nil {
		tx.CompletedAt = patch.CompletedAt
	}
	return true, nil
}

func (f *FakeTransactionRepo) Touch(_ context.Context, id uuid.UUID, status models.TransactionStatus, observed, now time.Time) (bool, error) {
	f.Touches = append(f.Touches, TouchCall{ID: id, Status: status, Observed: observed, Now: now})
	if len(f.TouchResults) > 0 {
		result := f.TouchResults[0]
		f.TouchResults = f.TouchResults[1:]
		return result.Applied, result.Err
	}
	if f.Err != nil {
		return false, f.Err
	}
	tx, ok := f.Transactions[id]
	if !ok || tx.Status != status || !tx.UpdatedAt.Equal(observed) {
		return false, nil
	}
	tx.UpdatedAt = now
	return true, nil
}

func (f *FakeTransactionRepo) ListStale(_ context.Context, before time.Time, maxAge time.Duration, limit int) ([]*models.Transaction, error) {
	f.StaleMaxAges = append(f.StaleMaxAges, maxAge)
	if f.StaleErr != nil {
		return nil, f.StaleErr
	}
	if f.Err != nil {
		return nil, f.Err
	}
	result := []*models.Transaction{}
	for _, tx := range f.Transactions {
		switch tx.Status {
		case models.StatusPending, models.StatusDebited, models.StatusReversing:
			if tx.UpdatedAt.Before(before) {
				copied := *tx
				result = append(result, &copied)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type FakeStore struct {
	Seen map[uuid.UUID]bool
	Errs []error
}

func NewFakeStore() *FakeStore {
	return &FakeStore{Seen: map[uuid.UUID]bool{}}
}

func (f *FakeStore) MarkProcessed(_ context.Context, eventID uuid.UUID) (bool, error) {
	if len(f.Errs) > 0 {
		err := f.Errs[0]
		f.Errs = f.Errs[1:]
		if err != nil {
			return false, err
		}
	}
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
	Err        error
	ErrByTopic map[string]error
	Published  []PublishedEvent
}

func (f *FakePublisher) Publish(_ context.Context, topic, key string, event *events.Event) error {
	if f.Err != nil {
		return f.Err
	}
	if err, ok := f.ErrByTopic[topic]; ok {
		return err
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

func Ptr(s string) *string {
	return &s
}
