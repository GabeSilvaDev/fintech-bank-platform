package tests

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
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
		return nil, domain.ErrNotFound
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
		return nil, domain.ErrNotFound
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

type FakeOperationRepo struct {
	Operations      map[string]*models.BalanceOperation
	ReserveErr      error
	GetErr          error
	CompleteErr     error
	ReleaseErr      error
	ReserveCalls    int
	GetCalls        int
	CompleteCalls   int
	ReleaseCalls    int
	ReleaseCtxErr   error
	ReleaseDeadline time.Time
}

func NewFakeOperationRepo() *FakeOperationRepo {
	return &FakeOperationRepo{Operations: map[string]*models.BalanceOperation{}}
}

func operationKey(accountID uuid.UUID, key string) string {
	return accountID.String() + "/" + key
}

func (f *FakeOperationRepo) Reserve(_ context.Context, accountID uuid.UUID, key, kind string, at time.Time) (bool, error) {
	f.ReserveCalls++
	if f.ReserveErr != nil {
		return false, f.ReserveErr
	}
	k := operationKey(accountID, key)
	if _, exists := f.Operations[k]; exists {
		return false, nil
	}
	f.Operations[k] = &models.BalanceOperation{AccountID: accountID, Key: key, Kind: kind, Status: models.OperationPending, CreatedAt: at, UpdatedAt: at}
	return true, nil
}

func (f *FakeOperationRepo) Get(_ context.Context, accountID uuid.UUID, key string) (*models.BalanceOperation, error) {
	f.GetCalls++
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	operation, ok := f.Operations[operationKey(accountID, key)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *operation
	return &copied, nil
}

func (f *FakeOperationRepo) Complete(_ context.Context, accountID uuid.UUID, key, result string, at time.Time) error {
	f.CompleteCalls++
	if f.CompleteErr != nil {
		return f.CompleteErr
	}
	operation, ok := f.Operations[operationKey(accountID, key)]
	if !ok || operation.Status != models.OperationPending {
		return nil
	}
	operation.Status = models.OperationDone
	operation.Result = result
	operation.UpdatedAt = at
	return nil
}

func (f *FakeOperationRepo) Release(ctx context.Context, accountID uuid.UUID, key string) error {
	f.ReleaseCalls++
	f.ReleaseCtxErr = ctx.Err()
	f.ReleaseDeadline, _ = ctx.Deadline()
	if f.ReleaseErr != nil {
		return f.ReleaseErr
	}
	k := operationKey(accountID, key)
	if operation, ok := f.Operations[k]; ok && operation.Status == models.OperationPending {
		delete(f.Operations, k)
	}
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

type FakeIdentityRepo struct {
	Identities map[string]*models.Identity
	Created    []*models.Identity
	CreateErr  error
	GetErr     error
}

func NewFakeIdentityRepo() *FakeIdentityRepo {
	return &FakeIdentityRepo{Identities: map[string]*models.Identity{}}
}

func (f *FakeIdentityRepo) Create(_ context.Context, identity *models.Identity) (bool, error) {
	if f.CreateErr != nil {
		return false, f.CreateErr
	}
	if _, exists := f.Identities[identity.Email]; exists {
		return false, nil
	}
	copied := *identity
	f.Identities[identity.Email] = &copied
	f.Created = append(f.Created, identity)
	return true, nil
}

func (f *FakeIdentityRepo) GetByEmail(_ context.Context, email string) (*models.Identity, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	identity, ok := f.Identities[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *identity
	return &copied, nil
}

type Comparison struct {
	Hash     string
	Password string
}

type FakeHasher struct {
	HashErr     error
	Hashed      []string
	Comparisons []Comparison
	OnHash      func()
}

func (f *FakeHasher) Hash(password string) (string, error) {
	if f.OnHash != nil {
		f.OnHash()
	}
	if f.HashErr != nil {
		return "", f.HashErr
	}
	f.Hashed = append(f.Hashed, password)
	return "hashed:" + password, nil
}

func (f *FakeHasher) Compare(hash, password string) error {
	f.Comparisons = append(f.Comparisons, Comparison{Hash: hash, Password: password})
	if hash != "hashed:"+password {
		return errors.New("mismatch")
	}
	return nil
}

type FakeRefreshTokenRepo struct {
	Tokens            map[string]*models.RefreshToken
	Families          map[uuid.UUID][]string
	Markers           map[uuid.UUID]bool
	Created           []*models.RefreshToken
	TTLs              []time.Duration
	MarkTTLs          []time.Duration
	RevokeTTLs        []time.Duration
	RevokedFamilies   []uuid.UUID
	CreateErr         error
	GetErr            error
	MarkErr           error
	RevokeErr         error
	FamilyRevokedErrs []error
	MarkApplied       *bool
	OnCreate          func()
	OnMark            func()
	AfterMark         func()
}

func NewFakeRefreshTokenRepo() *FakeRefreshTokenRepo {
	return &FakeRefreshTokenRepo{Tokens: map[string]*models.RefreshToken{}, Families: map[uuid.UUID][]string{}, Markers: map[uuid.UUID]bool{}}
}

func (f *FakeRefreshTokenRepo) Create(_ context.Context, token *models.RefreshToken, ttl time.Duration) error {
	if f.CreateErr != nil {
		return f.CreateErr
	}
	copied := *token
	f.Tokens[token.TokenHash] = &copied
	f.Families[token.FamilyID] = append(f.Families[token.FamilyID], token.TokenHash)
	f.Created = append(f.Created, token)
	f.TTLs = append(f.TTLs, ttl)
	if f.OnCreate != nil {
		f.OnCreate()
	}
	return nil
}

func (f *FakeRefreshTokenRepo) Get(_ context.Context, tokenHash string) (*models.RefreshToken, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	token, ok := f.Tokens[tokenHash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *token
	return &copied, nil
}

func (f *FakeRefreshTokenRepo) MarkRotated(_ context.Context, tokenHash string, ttl time.Duration) (bool, error) {
	f.MarkTTLs = append(f.MarkTTLs, ttl)
	if f.OnMark != nil {
		f.OnMark()
	}
	if f.AfterMark != nil {
		defer f.AfterMark()
	}
	if f.MarkErr != nil {
		return false, f.MarkErr
	}
	if f.MarkApplied != nil {
		return *f.MarkApplied, nil
	}
	token, ok := f.Tokens[tokenHash]
	if !ok || token.Status != models.RefreshTokenActive {
		return false, nil
	}
	token.Status = models.RefreshTokenRotated
	return true, nil
}

func (f *FakeRefreshTokenRepo) RevokeFamily(_ context.Context, familyID uuid.UUID, ttl time.Duration) error {
	if f.RevokeErr != nil {
		return f.RevokeErr
	}
	f.Markers[familyID] = true
	f.RevokedFamilies = append(f.RevokedFamilies, familyID)
	f.RevokeTTLs = append(f.RevokeTTLs, ttl)
	f.RevokeHashes(f.Families[familyID])
	return nil
}

func (f *FakeRefreshTokenRepo) RevokeHashes(hashes []string) {
	for _, hash := range hashes {
		if token := f.Tokens[hash]; token.Status == models.RefreshTokenActive {
			token.Status = models.RefreshTokenRevoked
		}
	}
}

func (f *FakeRefreshTokenRepo) FamilyRevoked(_ context.Context, familyID uuid.UUID) (bool, error) {
	if len(f.FamilyRevokedErrs) > 0 {
		err := f.FamilyRevokedErrs[0]
		f.FamilyRevokedErrs = f.FamilyRevokedErrs[1:]
		if err != nil {
			return false, err
		}
	}
	return f.Markers[familyID], nil
}

func (f *FakeRefreshTokenRepo) StatusOf(hash string) models.RefreshTokenStatus {
	return f.Tokens[hash].Status
}

func (f *FakeRefreshTokenRepo) ActiveIn(familyID uuid.UUID) []string {
	active := []string{}
	for _, hash := range f.Families[familyID] {
		if f.Tokens[hash].Status == models.RefreshTokenActive {
			active = append(active, hash)
		}
	}
	return active
}

type FailureWrite struct {
	Kind    string
	Current *models.LoginFailure
	Next    models.LoginFailure
	TTL     time.Duration
	Applied bool
}

type FakeLoginFailureRepo struct {
	mu          sync.Mutex
	Rows        map[string]models.LoginFailure
	Writes      []FailureWrite
	Cleared     []string
	GetCalls    int
	GetErr      error
	CreateErr   error
	ReplaceErr  error
	ClearErr    error
	BeforeWrite func(rows map[string]models.LoginFailure)
	Precision   time.Duration
}

func (f *FakeLoginFailureRepo) stored(failure models.LoginFailure) models.LoginFailure {
	if f.Precision > 0 {
		failure.FirstFailure = failure.FirstFailure.Truncate(f.Precision)
	}
	return failure
}

func NewFakeLoginFailureRepo() *FakeLoginFailureRepo {
	return &FakeLoginFailureRepo{Rows: map[string]models.LoginFailure{}}
}

func (f *FakeLoginFailureRepo) Get(_ context.Context, email string) (*models.LoginFailure, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetCalls++
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	row, ok := f.Rows[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &row, nil
}

func (f *FakeLoginFailureRepo) Create(_ context.Context, failure *models.LoginFailure, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.BeforeWrite != nil {
		f.BeforeWrite(f.Rows)
	}
	if f.CreateErr != nil {
		return false, f.CreateErr
	}
	_, exists := f.Rows[failure.Email]
	if !exists {
		f.Rows[failure.Email] = f.stored(*failure)
	}
	f.Writes = append(f.Writes, FailureWrite{Kind: "create", Next: *failure, TTL: ttl, Applied: !exists})
	return !exists, nil
}

func (f *FakeLoginFailureRepo) Replace(_ context.Context, current, next *models.LoginFailure, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.BeforeWrite != nil {
		f.BeforeWrite(f.Rows)
	}
	if f.ReplaceErr != nil {
		return false, f.ReplaceErr
	}
	row, exists := f.Rows[current.Email]
	applied := exists && row.Failures == current.Failures && row.FirstFailure.Equal(f.stored(*current).FirstFailure)
	if applied {
		f.Rows[current.Email] = f.stored(*next)
	}
	copied := *current
	f.Writes = append(f.Writes, FailureWrite{Kind: "replace", Current: &copied, Next: *next, TTL: ttl, Applied: applied})
	return applied, nil
}

func (f *FakeLoginFailureRepo) Clear(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cleared = append(f.Cleared, email)
	if f.ClearErr != nil {
		return f.ClearErr
	}
	delete(f.Rows, email)
	return nil
}

func (f *FakeLoginFailureRepo) Row(email string) (models.LoginFailure, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.Rows[email]
	return row, ok
}
