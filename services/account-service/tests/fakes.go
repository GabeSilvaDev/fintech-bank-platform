package tests

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
)

type StatusChange struct {
	AccountID uuid.UUID
	Status    models.AccountStatus
	UpdatedAt time.Time
	ClosedAt  *time.Time
}

type CASResult struct {
	Applied bool
	Err     error
}

type FakeAccountRepo struct {
	Accounts        map[uuid.UUID]*models.Account
	Reserved        map[string]uuid.UUID
	Created         []*models.Account
	Statuses        []StatusChange
	CASCalls        int
	CASResults      []CASResult
	OnCAS           func()
	CloseCalls      int
	CloseErr        error
	OnClose         func()
	GetErrs         []error
	CreateErr       error
	UpdateStatusErr error
	Err             error
}

func NewFakeAccountRepo() *FakeAccountRepo {
	return &FakeAccountRepo{Accounts: map[uuid.UUID]*models.Account{}, Reserved: map[string]uuid.UUID{}}
}

func (f *FakeAccountRepo) Put(account *models.Account) {
	copied := *account
	f.Accounts[account.AccountID] = &copied
}

func (f *FakeAccountRepo) Create(_ context.Context, account *models.Account) error {
	if f.CreateErr != nil {
		return f.CreateErr
	}
	if f.Err != nil {
		return f.Err
	}
	f.Put(account)
	f.Created = append(f.Created, account)
	return nil
}

func (f *FakeAccountRepo) ReserveNumber(_ context.Context, agency, number string, accountID uuid.UUID) (bool, error) {
	if f.Err != nil {
		return false, f.Err
	}
	key := agency + "-" + number
	if _, taken := f.Reserved[key]; taken {
		return false, nil
	}
	f.Reserved[key] = accountID
	return true, nil
}

func (f *FakeAccountRepo) Get(_ context.Context, accountID uuid.UUID) (*models.Account, error) {
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
	account, ok := f.Accounts[accountID]
	if !ok {
		return nil, models.ErrNotFound
	}
	copied := *account
	return &copied, nil
}

func (f *FakeAccountRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]*models.Account, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	result := []*models.Account{}
	for _, account := range f.Accounts {
		if account.UserID == userID {
			copied := *account
			result = append(result, &copied)
		}
	}
	return result, nil
}

func (f *FakeAccountRepo) UpdateStatus(_ context.Context, accountID uuid.UUID, status models.AccountStatus, updatedAt time.Time, closedAt *time.Time) error {
	if f.UpdateStatusErr != nil {
		return f.UpdateStatusErr
	}
	if f.Err != nil {
		return f.Err
	}
	account := f.Accounts[accountID]
	account.Status = status
	account.UpdatedAt = updatedAt
	account.ClosedAt = closedAt
	f.Statuses = append(f.Statuses, StatusChange{AccountID: accountID, Status: status, UpdatedAt: updatedAt, ClosedAt: closedAt})
	return nil
}

func (f *FakeAccountRepo) CloseIfEmpty(_ context.Context, accountID uuid.UUID, closedAt time.Time) (bool, error) {
	f.CloseCalls++
	if f.OnClose != nil {
		f.OnClose()
	}
	if f.CloseErr != nil {
		return false, f.CloseErr
	}
	if f.Err != nil {
		return false, f.Err
	}
	account := f.Accounts[accountID]
	if account.BalanceCents != 0 || account.Status == models.AccountStatusClosed {
		return false, nil
	}
	account.Status = models.AccountStatusClosed
	account.UpdatedAt = closedAt
	account.ClosedAt = &closedAt
	f.Statuses = append(f.Statuses, StatusChange{AccountID: accountID, Status: models.AccountStatusClosed, UpdatedAt: closedAt, ClosedAt: &closedAt})
	return true, nil
}

func (f *FakeAccountRepo) CompareAndSetBalance(_ context.Context, accountID uuid.UUID, expected, next int64, updatedAt time.Time) (bool, error) {
	f.CASCalls++
	if f.OnCAS != nil {
		f.OnCAS()
	}
	if len(f.CASResults) > 0 {
		result := f.CASResults[0]
		f.CASResults = f.CASResults[1:]
		return result.Applied, result.Err
	}
	if f.Err != nil {
		return false, f.Err
	}
	account := f.Accounts[accountID]
	if account.BalanceCents != expected || account.Status != models.AccountStatusActive {
		return false, nil
	}
	account.BalanceCents = next
	account.UpdatedAt = updatedAt
	return true, nil
}

type ProfileChange struct {
	UserID    uuid.UUID
	Name      *string
	Email     *string
	Phone     *string
	UpdatedAt time.Time
}

type FakeCustomerRepo struct {
	Customers map[uuid.UUID]*models.Customer
	Upserts   []*models.Customer
	Profiles  []ProfileChange
	Err       error
}

func NewFakeCustomerRepo() *FakeCustomerRepo {
	return &FakeCustomerRepo{Customers: map[uuid.UUID]*models.Customer{}}
}

func (f *FakeCustomerRepo) Put(customer *models.Customer) {
	copied := *customer
	f.Customers[customer.UserID] = &copied
}

func (f *FakeCustomerRepo) Upsert(_ context.Context, customer *models.Customer) error {
	if f.Err != nil {
		return f.Err
	}
	f.Put(customer)
	f.Upserts = append(f.Upserts, customer)
	return nil
}

func (f *FakeCustomerRepo) Get(_ context.Context, userID uuid.UUID) (*models.Customer, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	customer, ok := f.Customers[userID]
	if !ok {
		return nil, models.ErrNotFound
	}
	copied := *customer
	return &copied, nil
}

func (f *FakeCustomerRepo) UpdateProfile(_ context.Context, userID uuid.UUID, name, email, phone *string, updatedAt time.Time) error {
	if f.Err != nil {
		return f.Err
	}
	customer := f.Customers[userID]
	if name != nil {
		customer.Name = *name
	}
	if email != nil {
		customer.Email = *email
	}
	if phone != nil {
		customer.Phone = *phone
	}
	customer.UpdatedAt = updatedAt
	f.Profiles = append(f.Profiles, ProfileChange{UserID: userID, Name: name, Email: email, Phone: phone, UpdatedAt: updatedAt})
	return nil
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
