package unit

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gatedHasher struct {
	mu      sync.Mutex
	gate    chan struct{}
	entered chan string
	calls   []string
}

func (h *gatedHasher) pass(call string) {
	h.mu.Lock()
	h.calls = append(h.calls, call)
	gate := h.gate
	h.mu.Unlock()
	if gate != nil {
		h.entered <- call
		<-gate
	}
}

func (h *gatedHasher) Hash(password string) (string, error) {
	h.pass("hash")
	return "hashed:" + password, nil
}

func (h *gatedHasher) Compare(hash, password string) error {
	h.pass("compare")
	if hash != "hashed:"+password {
		return errors.New("mismatch")
	}
	return nil
}

func (h *gatedHasher) close() {
	h.mu.Lock()
	h.gate = make(chan struct{}, 1)
	h.entered = make(chan string, 16)
	h.mu.Unlock()
}

func (h *gatedHasher) open() {
	h.mu.Lock()
	close(h.gate)
	h.mu.Unlock()
}

func (h *gatedHasher) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

func newGatedService(t *testing.T, capacity int, wait time.Duration) (*services.IdentityService, *gatedHasher, *tests.FakeIdentityRepo) {
	t.Helper()
	hasher := &gatedHasher{}
	repo := tests.NewFakeIdentityRepo()
	service, err := services.NewIdentityService(repo, hasher, tests.FakeClock{T: now})
	require.NoError(t, err)
	return service.WithHashConcurrency(capacity, wait), hasher, repo
}

func occupy(t *testing.T, service *services.IdentityService, hasher *gatedHasher) <-chan error {
	t.Helper()
	hasher.close()
	done := make(chan error, 1)
	go func() {
		_, err := service.Register(context.Background(), "first@example.com", "correct horse")
		done <- err
	}()
	select {
	case <-hasher.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first hash never started")
	}
	return done
}

func TestIdentityServiceRefusesHashingBeyondItsCapacity(t *testing.T) {
	service, hasher, repo := newGatedService(t, 1, 20*time.Millisecond)
	repo.Identities["ana@example.com"] = &models.Identity{Email: "ana@example.com", UserID: uuid.New(), PasswordHash: "hashed:correct horse"}
	done := occupy(t, service, hasher)
	before := hasher.count()

	_, err := service.Register(context.Background(), "second@example.com", "correct horse")
	assert.ErrorIs(t, err, services.ErrBusy)
	_, err = service.Verify(context.Background(), "ana@example.com", "correct horse")
	assert.ErrorIs(t, err, services.ErrBusy)
	_, err = service.Verify(context.Background(), "ghost@example.com", "correct horse")
	assert.ErrorIs(t, err, services.ErrBusy)
	_, err = service.Verify(context.Background(), "not-an-email", "correct horse")
	assert.ErrorIs(t, err, services.ErrBusy)
	assert.Equal(t, before, hasher.count())
	assert.Len(t, repo.Created, 0)

	hasher.open()
	require.NoError(t, <-done)

	_, err = service.Verify(context.Background(), "ghost@example.com", "correct horse")
	assert.ErrorIs(t, err, services.ErrInvalidCredentials)
	userID, err := service.Verify(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)
	assert.Equal(t, repo.Identities["ana@example.com"].UserID, userID)
}

func TestIdentityServiceStopsWaitingWhenTheRequestIsCancelled(t *testing.T) {
	service, hasher, _ := newGatedService(t, 1, time.Minute)
	done := occupy(t, service, hasher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := service.Verify(ctx, "ana@example.com", "correct horse")

	assert.ErrorIs(t, err, services.ErrBusy)
	assert.Less(t, time.Since(start), 5*time.Second)
	hasher.open()
	require.NoError(t, <-done)
}

func TestIdentityServiceWaitsForAFreeSlot(t *testing.T) {
	service, hasher, _ := newGatedService(t, 1, 5*time.Second)
	done := occupy(t, service, hasher)
	result := make(chan error, 1)
	go func() {
		_, err := service.Register(context.Background(), "second@example.com", "correct horse")
		result <- err
	}()

	hasher.open()

	require.NoError(t, <-done)
	require.NoError(t, <-result)
}

func TestIdentityServiceKeepsAtLeastOneHashingSlot(t *testing.T) {
	service, _, _ := newGatedService(t, 0, time.Second)

	_, err := service.Register(context.Background(), "ana@example.com", "correct horse")

	assert.NoError(t, err)
}

func TestIdentityServiceDefaultHashingCapacity(t *testing.T) {
	assert.Equal(t, 2*runtime.GOMAXPROCS(0), services.DefaultHashConcurrency())
	assert.Equal(t, 2*time.Second, services.HashWait)
}
