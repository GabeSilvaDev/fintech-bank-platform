package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/directory"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func newOwnerServer(t *testing.T, calls map[uuid.UUID]*int32, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		accountID := uuid.MustParse(parts[2])
		mu.Lock()
		counter, ok := calls[accountID]
		if !ok {
			var zero int32
			counter = &zero
			calls[accountID] = counter
		}
		mu.Unlock()
		atomic.AddInt32(counter, 1)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"account_id": accountID.String(),
				"user_id":    uuid.New().String(),
			},
		})
	}))
}

func TestDirectoryLookupCachesUntilTTLExpires(t *testing.T) {
	accountID := uuid.New()
	userID := uuid.New()
	calls := 0
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"account_id": accountID.String(),
				"user_id":    userID.String(),
				"name":       "Ana Souza",
				"email":      "ana@example.com",
				"phone":      "+5511999887766",
			},
		})
	}))
	defer server.Close()

	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	client := directory.NewClient(server.URL, time.Second, time.Minute).WithClock(func() time.Time { return clock })

	contact, err := client.Lookup(context.Background(), accountID)
	assert.NoError(t, err)
	assert.Equal(t, accountID, contact.AccountID)
	assert.Equal(t, userID, contact.UserID)
	assert.Equal(t, "Ana Souza", contact.Name)
	assert.Equal(t, "ana@example.com", contact.Email)
	assert.Equal(t, "+5511999887766", contact.Phone)
	assert.Equal(t, "/accounts/"+accountID.String()+"/owner", gotPath)
	assert.Equal(t, 1, calls)

	_, err = client.Lookup(context.Background(), accountID)
	assert.NoError(t, err)
	assert.Equal(t, 1, calls)

	clock = clock.Add(time.Minute + time.Second)
	_, err = client.Lookup(context.Background(), accountID)
	assert.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestDirectoryLookupNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   map[string]string{"code": "ACCOUNT_NOT_FOUND"},
		})
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	_, err := client.Lookup(context.Background(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDirectoryLookupServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	_, err := client.Lookup(context.Background(), uuid.New())
	assert.ErrorContains(t, err, "500")
}

func TestDirectoryLookupMalformedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	_, err := client.Lookup(context.Background(), uuid.New())
	assert.Error(t, err)
}

func TestDirectoryLookupInvalidUserID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"account_id": uuid.NewString(),
				"user_id":    "x",
			},
		})
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	_, err := client.Lookup(context.Background(), uuid.New())
	assert.Error(t, err)
}

func TestDirectoryLookupTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	_, err := client.Lookup(context.Background(), uuid.New())
	assert.Error(t, err)
}

func TestDirectoryLookupRejectsOversizedBodies(t *testing.T) {
	accountID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{
				"account_id": accountID.String(),
				"user_id":    uuid.NewString(),
				"name":       strings.Repeat("a", 2<<20),
			},
		})
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	_, err := client.Lookup(context.Background(), accountID)
	assert.Error(t, err)
}

func TestDirectoryLookupPropagatesTraceContext(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	provider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	accountID := uuid.New()
	var gotTraceParent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceParent = r.Header.Get("traceparent")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"account_id": accountID.String(), "user_id": uuid.NewString()},
		})
	}))
	defer server.Close()

	ctx, span := tracing.Tracer().Start(context.Background(), "route notification")
	defer span.End()

	_, err := directory.NewClient(server.URL, time.Second, time.Minute).Lookup(ctx, accountID)

	require.NoError(t, err)
	require.NotEmpty(t, gotTraceParent)
	assert.Contains(t, gotTraceParent, span.SpanContext().TraceID().String())
}

func TestDirectoryCacheEvictsOldestEntryWhenFull(t *testing.T) {
	var mu sync.Mutex
	calls := map[uuid.UUID]*int32{}
	server := newOwnerServer(t, calls, &mu)
	defer server.Close()

	accountA, accountB, accountC := uuid.New(), uuid.New(), uuid.New()
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	client := directory.NewClient(server.URL, time.Second, time.Hour).WithClock(func() time.Time { return clock }).WithMaxEntries(2)

	_, err := client.Lookup(context.Background(), accountA)
	require.NoError(t, err)
	clock = clock.Add(time.Second)
	_, err = client.Lookup(context.Background(), accountB)
	require.NoError(t, err)
	clock = clock.Add(time.Second)
	_, err = client.Lookup(context.Background(), accountC)
	require.NoError(t, err)

	_, err = client.Lookup(context.Background(), accountB)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(calls[accountB]))

	_, err = client.Lookup(context.Background(), accountA)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(calls[accountA]))
	assert.Equal(t, int32(1), atomic.LoadInt32(calls[accountC]))
}

func TestDirectoryCacheDropsExpiredEntriesBeforeOldest(t *testing.T) {
	var mu sync.Mutex
	calls := map[uuid.UUID]*int32{}
	server := newOwnerServer(t, calls, &mu)
	defer server.Close()

	accountA, accountB, accountC := uuid.New(), uuid.New(), uuid.New()
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	client := directory.NewClient(server.URL, time.Second, time.Minute).WithClock(func() time.Time { return clock }).WithMaxEntries(2)

	_, err := client.Lookup(context.Background(), accountA)
	require.NoError(t, err)
	_, err = client.Lookup(context.Background(), accountB)
	require.NoError(t, err)

	clock = clock.Add(2 * time.Minute)
	_, err = client.Lookup(context.Background(), accountC)
	require.NoError(t, err)

	_, err = client.Lookup(context.Background(), accountC)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(calls[accountC]))

	_, err = client.Lookup(context.Background(), accountA)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(calls[accountA]))
}

func TestDirectoryLookupConcurrentCallsCollapseIntoOneRequest(t *testing.T) {
	accountID := uuid.New()
	var calls int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		<-release
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"account_id": accountID.String(),
				"user_id":    uuid.New().String(),
			},
		})
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, 5*time.Second, time.Minute)

	const workers = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := client.Lookup(context.Background(), accountID)
			errs[i] = err
		}(i)
	}
	close(start)
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestDirectoryLookupCancelledCallerDoesNotAffectOthers(t *testing.T) {
	accountID := uuid.New()
	var calls int32
	requestStarted := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		close(requestStarted)
		<-release
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"account_id": accountID.String(),
				"user_id":    uuid.New().String(),
			},
		})
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, 5*time.Second, time.Minute)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancelledErr := make(chan error, 1)
	go func() {
		_, err := client.Lookup(cancelledCtx, accountID)
		cancelledErr <- err
	}()

	patientErr := make(chan error, 1)
	go func() {
		<-requestStarted
		_, err := client.Lookup(context.Background(), accountID)
		patientErr <- err
	}()

	<-requestStarted
	cancel()
	assert.ErrorIs(t, <-cancelledErr, context.Canceled)

	close(release)
	assert.NoError(t, <-patientErr)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestDirectoryLookupErrorsAreNotCached(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := directory.NewClient(server.URL, time.Second, time.Minute)
	accountID := uuid.New()

	_, err := client.Lookup(context.Background(), accountID)
	assert.Error(t, err)
	_, err = client.Lookup(context.Background(), accountID)
	assert.Error(t, err)

	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}
