package unit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type identityHarness struct {
	repo    *tests.FakeIdentityRepo
	hasher  *tests.FakeHasher
	service *services.IdentityService
}

func newIdentityHarness(t *testing.T) *identityHarness {
	h := &identityHarness{repo: tests.NewFakeIdentityRepo(), hasher: &tests.FakeHasher{}}
	service, err := services.NewIdentityService(h.repo, h.hasher, tests.FakeClock{T: now})
	require.NoError(t, err)
	h.service = service
	return h
}

func TestNewIdentityServiceHashesDummyPasswordOnce(t *testing.T) {
	h := newIdentityHarness(t)

	assert.Len(t, h.hasher.Hashed, 1)
}

func TestNewIdentityServiceFailsWhenHasherFails(t *testing.T) {
	service, err := services.NewIdentityService(tests.NewFakeIdentityRepo(), &tests.FakeHasher{HashErr: errors.New("boom")}, tests.FakeClock{T: now})

	assert.Nil(t, service)
	assert.EqualError(t, err, "boom")
}

func TestRegisterNormalisesEmailAndStoresHash(t *testing.T) {
	h := newIdentityHarness(t)

	userID, err := h.service.Register(context.Background(), "  Ana.Souza@Example.COM ", "correct horse")

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, userID)
	require.Len(t, h.repo.Created, 1)
	stored := h.repo.Created[0]
	assert.Equal(t, "ana.souza@example.com", stored.Email)
	assert.Equal(t, userID, stored.UserID)
	assert.Equal(t, "hashed:correct horse", stored.PasswordHash)
	assert.Equal(t, now, stored.CreatedAt)
}

func TestRegisterRejectsInvalidEmail(t *testing.T) {
	for _, email := range []string{"", "   ", "not-an-email", "ana@"} {
		h := newIdentityHarness(t)

		_, err := h.service.Register(context.Background(), email, "correct horse")

		assert.Equal(t, "invalid_email", domain.InvalidCode(err), email)
		assert.Empty(t, h.repo.Created)
	}
}

func TestRegisterRejectsPasswordOutsideLength(t *testing.T) {
	for _, password := range []string{"", "1234567", strings.Repeat("a", 73)} {
		h := newIdentityHarness(t)

		_, err := h.service.Register(context.Background(), "ana@example.com", password)

		assert.Equal(t, "invalid_password", domain.InvalidCode(err))
		assert.Empty(t, h.repo.Created)
	}
}

func TestRegisterAcceptsPasswordLengthBounds(t *testing.T) {
	for i, password := range []string{"12345678", strings.Repeat("a", 72)} {
		h := newIdentityHarness(t)

		_, err := h.service.Register(context.Background(), "user"+string(rune('a'+i))+"@example.com", password)

		assert.NoError(t, err)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	h := newIdentityHarness(t)
	_, err := h.service.Register(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)

	_, err = h.service.Register(context.Background(), " ANA@example.com", "another password")

	assert.ErrorIs(t, err, services.ErrEmailTaken)
	assert.Len(t, h.repo.Created, 1)
}

func TestRegisterPropagatesHashError(t *testing.T) {
	h := newIdentityHarness(t)
	h.hasher.HashErr = errors.New("hash failed")

	_, err := h.service.Register(context.Background(), "ana@example.com", "correct horse")

	assert.EqualError(t, err, "hash failed")
}

func TestRegisterPropagatesRepositoryError(t *testing.T) {
	h := newIdentityHarness(t)
	h.repo.CreateErr = domain.ErrAmbiguousWrite

	_, err := h.service.Register(context.Background(), "ana@example.com", "correct horse")

	assert.ErrorIs(t, err, domain.ErrAmbiguousWrite)
}

func TestVerifyReturnsUserIDForValidCredentials(t *testing.T) {
	h := newIdentityHarness(t)
	userID, err := h.service.Register(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)

	verified, err := h.service.Verify(context.Background(), "  ANA@Example.com", "correct horse")

	require.NoError(t, err)
	assert.Equal(t, userID, verified)
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	h := newIdentityHarness(t)
	_, err := h.service.Register(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)

	verified, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

	assert.ErrorIs(t, err, services.ErrInvalidCredentials)
	assert.Equal(t, uuid.Nil, verified)
}

func TestVerifyComparesAgainstDummyHashForUnknownEmail(t *testing.T) {
	h := newIdentityHarness(t)
	dummyHash := "hashed:" + h.hasher.Hashed[0]

	verified, err := h.service.Verify(context.Background(), "ghost@example.com", "correct horse")

	assert.ErrorIs(t, err, services.ErrInvalidCredentials)
	assert.Equal(t, uuid.Nil, verified)
	require.Len(t, h.hasher.Comparisons, 1)
	assert.Equal(t, tests.Comparison{Hash: dummyHash, Password: "correct horse"}, h.hasher.Comparisons[0])
}

func TestVerifyPropagatesRepositoryError(t *testing.T) {
	h := newIdentityHarness(t)
	h.repo.GetErr = errors.New("cassandra down")

	_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	assert.EqualError(t, err, "cassandra down")
	assert.Empty(t, h.hasher.Comparisons)
}

func TestVerifyUsesStoredIdentity(t *testing.T) {
	h := newIdentityHarness(t)
	userID := uuid.New()
	h.repo.Identities["ana@example.com"] = &models.Identity{Email: "ana@example.com", UserID: userID, PasswordHash: "hashed:secret123"}

	verified, err := h.service.Verify(context.Background(), "ana@example.com", "secret123")

	require.NoError(t, err)
	assert.Equal(t, userID, verified)
}

func TestVerifyRejectsUnusableEmailWithDummyCompare(t *testing.T) {
	for _, email := range []string{"", "   ", "not-an-email"} {
		h := newIdentityHarness(t)
		h.repo.GetErr = errors.New("repository must not be called")
		dummyHash := "hashed:" + h.hasher.Hashed[0]

		verified, err := h.service.Verify(context.Background(), email, "correct horse")

		assert.ErrorIs(t, err, services.ErrInvalidCredentials, email)
		assert.Equal(t, uuid.Nil, verified)
		require.Len(t, h.hasher.Comparisons, 1)
		assert.Equal(t, tests.Comparison{Hash: dummyHash, Password: "correct horse"}, h.hasher.Comparisons[0])
	}
}

func TestVerifyRejectsPasswordLongerThanBcryptLimit(t *testing.T) {
	h := newIdentityHarness(t)
	long := strings.Repeat("a", 73)
	h.repo.Identities["ana@example.com"] = &models.Identity{Email: "ana@example.com", UserID: uuid.New(), PasswordHash: "hashed:" + long}

	verified, err := h.service.Verify(context.Background(), "ana@example.com", long)

	assert.ErrorIs(t, err, services.ErrInvalidCredentials)
	assert.Equal(t, uuid.Nil, verified)
	assert.Len(t, h.hasher.Comparisons, 1)
}

func TestVerifyWithBcryptRejectsSuffixBeyondSeventyTwoBytes(t *testing.T) {
	service, err := services.NewIdentityService(tests.NewFakeIdentityRepo(), services.NewBcryptHasher(bcrypt.MinCost), tests.FakeClock{T: now})
	require.NoError(t, err)
	password := strings.Repeat("a", 72)
	userID, err := service.Register(context.Background(), "ana@example.com", password)
	require.NoError(t, err)

	verified, err := service.Verify(context.Background(), "ana@example.com", password)
	require.NoError(t, err)
	assert.Equal(t, userID, verified)

	_, err = service.Verify(context.Background(), "ana@example.com", password+"suffix")
	assert.ErrorIs(t, err, services.ErrInvalidCredentials)
}

func TestBcryptHasherRoundTrip(t *testing.T) {
	hasher := services.NewBcryptHasher(bcrypt.MinCost)

	hash, err := hasher.Hash("correct horse")

	require.NoError(t, err)
	assert.NotEqual(t, "correct horse", hash)
	cost, err := bcrypt.Cost([]byte(hash))
	require.NoError(t, err)
	assert.Equal(t, bcrypt.MinCost, cost)
	assert.NoError(t, hasher.Compare(hash, "correct horse"))
	assert.Error(t, hasher.Compare(hash, "wrong horse"))
}

func TestBcryptHasherRejectsInvalidCost(t *testing.T) {
	_, err := services.NewBcryptHasher(bcrypt.MaxCost + 1).Hash("correct horse")

	assert.Error(t, err)
}

func TestBcryptCostIsTwelve(t *testing.T) {
	assert.Equal(t, 12, services.BcryptCost)
}
