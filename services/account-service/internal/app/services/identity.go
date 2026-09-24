package services

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	BcryptCost        = 12
	minPasswordLength = 8
	maxPasswordLength = 72
	dummyPassword     = "identity-verification-placeholder"
	HashWait          = 2 * time.Second
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrBusy               = errors.New("too many passwords are being hashed")
)

func DefaultHashConcurrency() int {
	return 2 * runtime.GOMAXPROCS(0)
}

type BcryptHasher struct {
	cost int
}

func NewBcryptHasher(cost int) BcryptHasher {
	return BcryptHasher{cost: cost}
}

func (h BcryptHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	return string(hash), err
}

func (h BcryptHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

type IdentityService struct {
	identities contracts.IdentityRepository
	hasher     contracts.Hasher
	clock      contracts.Clock
	dummyHash  string
	slots      chan struct{}
	wait       time.Duration
}

func NewIdentityService(identities contracts.IdentityRepository, hasher contracts.Hasher, clock contracts.Clock) (*IdentityService, error) {
	dummyHash, err := hasher.Hash(dummyPassword)
	if err != nil {
		return nil, err
	}
	service := &IdentityService{identities: identities, hasher: hasher, clock: clock, dummyHash: dummyHash}
	return service.WithHashConcurrency(DefaultHashConcurrency(), HashWait), nil
}

func (s *IdentityService) WithHashConcurrency(capacity int, wait time.Duration) *IdentityService {
	if capacity < 1 {
		capacity = 1
	}
	s.slots = make(chan struct{}, capacity)
	s.wait = wait
	return s
}

func (s *IdentityService) Register(ctx context.Context, email, password string) (uuid.UUID, error) {
	email = normaliseEmail(email)
	if validation.ValidateVar(email, "required,email") != nil {
		return uuid.Nil, domain.Invalid("invalid_email", "email is not valid")
	}
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return uuid.Nil, domain.Invalid("invalid_password", "password must have between 8 and 72 bytes")
	}

	hash, err := s.hash(ctx, password)
	if err != nil {
		return uuid.Nil, err
	}

	identity := &models.Identity{Email: email, UserID: uuid.New(), PasswordHash: hash, CreatedAt: s.clock.Now()}
	created, err := s.identities.Create(ctx, identity)
	if err != nil {
		return uuid.Nil, err
	}
	if !created {
		return uuid.Nil, ErrEmailTaken
	}
	return identity.UserID, nil
}

func (s *IdentityService) Verify(ctx context.Context, email, password string) (uuid.UUID, error) {
	email = normaliseEmail(email)
	if validation.ValidateVar(email, "required,email") != nil {
		return s.reject(ctx, password)
	}
	identity, err := s.identities.GetByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return s.reject(ctx, password)
	}
	if err != nil {
		return uuid.Nil, err
	}
	matched, err := s.matches(ctx, identity.PasswordHash, password)
	if err != nil {
		return uuid.Nil, err
	}
	if !matched || len(password) > maxPasswordLength {
		return uuid.Nil, ErrInvalidCredentials
	}
	return identity.UserID, nil
}

func (s *IdentityService) reject(ctx context.Context, password string) (uuid.UUID, error) {
	if _, err := s.matches(ctx, s.dummyHash, password); err != nil {
		return uuid.Nil, err
	}
	return uuid.Nil, ErrInvalidCredentials
}

func (s *IdentityService) hash(ctx context.Context, password string) (string, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return s.hasher.Hash(password)
}

func (s *IdentityService) matches(ctx context.Context, hash, password string) (bool, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return s.hasher.Compare(hash, password) == nil, nil
}

func (s *IdentityService) acquire(ctx context.Context) (func(), error) {
	timer := time.NewTimer(s.wait)
	defer timer.Stop()
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-timer.C:
		return nil, ErrBusy
	case <-ctx.Done():
		return nil, ErrBusy
	}
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
