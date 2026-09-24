package services

import (
	"context"
	"errors"
	"strings"

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
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

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
}

func NewIdentityService(identities contracts.IdentityRepository, hasher contracts.Hasher, clock contracts.Clock) (*IdentityService, error) {
	dummyHash, err := hasher.Hash(dummyPassword)
	if err != nil {
		return nil, err
	}
	return &IdentityService{identities: identities, hasher: hasher, clock: clock, dummyHash: dummyHash}, nil
}

func (s *IdentityService) Register(ctx context.Context, email, password string) (uuid.UUID, error) {
	email = NormaliseEmail(email)
	if validation.ValidateVar(email, "required,email") != nil {
		return uuid.Nil, domain.Invalid("invalid_email", "email is not valid")
	}
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return uuid.Nil, domain.Invalid("invalid_password", "password must have between 8 and 72 bytes")
	}

	hash, err := s.hasher.Hash(password)
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
	identity, err := s.identities.GetByEmail(ctx, NormaliseEmail(email))
	if errors.Is(err, domain.ErrNotFound) {
		_ = s.hasher.Compare(s.dummyHash, password)
		return uuid.Nil, ErrInvalidCredentials
	}
	if err != nil {
		return uuid.Nil, err
	}
	if s.hasher.Compare(identity.PasswordHash, password) != nil {
		return uuid.Nil, ErrInvalidCredentials
	}
	return identity.UserID, nil
}

func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
