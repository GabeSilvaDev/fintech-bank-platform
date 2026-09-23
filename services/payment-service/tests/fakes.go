package tests

import (
	"context"
	"sort"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
)

type TransitionCall struct {
	ID    uuid.UUID
	From  models.Status
	To    models.Status
	Patch models.Patch
}

type TransitionResult struct {
	Applied bool
	Err     error
}

type FakePaymentRepo struct {
	Payments          map[uuid.UUID]*models.Payment
	Keys              map[string]uuid.UUID
	External          map[string]uuid.UUID
	Created           []*models.Payment
	Transitions       []TransitionCall
	TransitionResults []TransitionResult
	GetErrs           []error
	Err               error
	CreateErr         error
	ReserveErr        error
	BindErr           error
	FindErr           error
}

func NewFakePaymentRepo() *FakePaymentRepo {
	return &FakePaymentRepo{Payments: map[uuid.UUID]*models.Payment{}, Keys: map[string]uuid.UUID{}, External: map[string]uuid.UUID{}}
}

func KeyOf(accountID uuid.UUID, key string) string {
	return accountID.String() + "/" + key
}

func (f *FakePaymentRepo) Put(payment *models.Payment) {
	copied := *payment
	f.Payments[payment.ID] = &copied
}

func (f *FakePaymentRepo) Create(_ context.Context, payment *models.Payment) error {
	if f.CreateErr != nil {
		return f.CreateErr
	}
	if f.Err != nil {
		return f.Err
	}
	f.Put(payment)
	f.Created = append(f.Created, payment)
	return nil
}

func (f *FakePaymentRepo) ReserveKey(_ context.Context, accountID uuid.UUID, key string, id uuid.UUID) (uuid.UUID, error) {
	if f.ReserveErr != nil {
		return uuid.Nil, f.ReserveErr
	}
	if f.Err != nil {
		return uuid.Nil, f.Err
	}
	if owner, taken := f.Keys[KeyOf(accountID, key)]; taken {
		return owner, nil
	}
	f.Keys[KeyOf(accountID, key)] = id
	return id, nil
}

func (f *FakePaymentRepo) Get(_ context.Context, id uuid.UUID) (*models.Payment, error) {
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
	payment, ok := f.Payments[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *payment
	return &copied, nil
}

func (f *FakePaymentRepo) ListByAccount(_ context.Context, accountID uuid.UUID, limit int) ([]*models.Payment, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	result := []*models.Payment{}
	for _, payment := range f.Payments {
		if payment.AccountID == accountID {
			copied := *payment
			result = append(result, &copied)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (f *FakePaymentRepo) Transition(_ context.Context, id uuid.UUID, from, to models.Status, patch models.Patch) (bool, error) {
	f.Transitions = append(f.Transitions, TransitionCall{ID: id, From: from, To: to, Patch: patch})
	if len(f.TransitionResults) > 0 {
		result := f.TransitionResults[0]
		f.TransitionResults = f.TransitionResults[1:]
		return result.Applied, result.Err
	}
	if f.Err != nil {
		return false, f.Err
	}
	payment, ok := f.Payments[id]
	if !ok || payment.Status != from {
		return false, nil
	}
	payment.Status = to
	payment.UpdatedAt = patch.UpdatedAt
	if patch.ExternalID != nil {
		payment.ExternalID = *patch.ExternalID
	}
	if patch.FailureReason != nil {
		payment.FailureReason = *patch.FailureReason
	}
	if patch.BalanceAfterCents != nil {
		payment.BalanceAfterCents = patch.BalanceAfterCents
	}
	if patch.CompletedAt != nil {
		payment.CompletedAt = patch.CompletedAt
	}
	return true, nil
}

func (f *FakePaymentRepo) BindExternalID(_ context.Context, externalID string, id uuid.UUID) error {
	if f.BindErr != nil {
		return f.BindErr
	}
	f.External[externalID] = id
	return nil
}

func (f *FakePaymentRepo) FindByExternalID(_ context.Context, externalID string) (uuid.UUID, error) {
	if f.FindErr != nil {
		return uuid.Nil, f.FindErr
	}
	id, ok := f.External[externalID]
	if !ok {
		return uuid.Nil, domain.ErrNotFound
	}
	return id, nil
}

type FakeGateway struct {
	Submissions []models.Submission
	Errs        []error
	Calls       []models.Payment
}

func (f *FakeGateway) Submit(_ context.Context, payment *models.Payment) (models.Submission, error) {
	f.Calls = append(f.Calls, *payment)
	if len(f.Errs) > 0 {
		err := f.Errs[0]
		f.Errs = f.Errs[1:]
		if err != nil {
			return models.Submission{}, err
		}
	}
	if len(f.Submissions) > 0 {
		submission := f.Submissions[0]
		f.Submissions = f.Submissions[1:]
		return submission, nil
	}
	return models.Submission{ExternalID: "ext-" + payment.ID.String(), Status: models.SubmissionSettled}, nil
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

func Ptr(s string) *string {
	return &s
}
