package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/owners"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ownerServer struct {
	mu        sync.Mutex
	owners    map[string]uuid.UUID
	status    int
	body      string
	calls     []string
	requestID string
	parent    string
}

func (s *ownerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	s.requestID = r.Header.Get(middleware.RequestIDHeader)
	s.parent = r.Header.Get("traceparent")

	w.Header().Set("Content-Type", "application/json")
	if s.status != 0 {
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
		return
	}
	for accountID, userID := range s.owners {
		if r.URL.Path == "/accounts/"+accountID+"/owner" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"account_id":"` + accountID + `","user_id":"` + userID.String() + `","name":"Ana","email":"ana@example.com"}}`))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"success":false,"error":{"code":"ACCOUNT_NOT_FOUND","message":"account not found"}}`))
}

func (s *ownerServer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func ownerClient(t *testing.T, server *ownerServer) (*owners.Client, *fakeClock) {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	base, err := url.Parse(httpServer.URL)
	require.NoError(t, err)
	clock := &fakeClock{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
	return owners.NewClient(base, time.Second, time.Minute).WithClock(clock.Now), clock
}

func newOwnerServer(pairs ...uuid.UUID) *ownerServer {
	server := &ownerServer{owners: make(map[string]uuid.UUID)}
	for i := 0; i+1 < len(pairs); i += 2 {
		server.owners[pairs[i].String()] = pairs[i+1]
	}
	return server
}

func TestOwnerClientReturnsTheOwnerOfTheAccount(t *testing.T) {
	recorder := useTraceRecorder(t)
	accountID, userID := uuid.New(), uuid.New()
	server := newOwnerServer(accountID, userID)
	client, _ := ownerClient(t, server)

	ctx, span := tracing.Tracer().Start(requestContext("req-owner"), "incoming request")
	got, err := client.Owner(ctx, accountID)
	span.End()

	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, []string{"GET /accounts/" + accountID.String() + "/owner"}, server.calls)
	assert.Equal(t, "req-owner", server.requestID)
	assert.Contains(t, server.parent, span.SpanContext().TraceID().String())
	assert.NotEmpty(t, recorder.Ended())
}

func TestOwnerClientCachesTheOwner(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newOwnerServer(accountID, userID)
	client, clock := ownerClient(t, server)

	for i := 0; i < 2; i++ {
		got, err := client.Owner(context.Background(), accountID)
		require.NoError(t, err)
		assert.Equal(t, userID, got)
	}
	assert.Equal(t, 1, server.callCount())

	clock.Advance(59 * time.Second)
	_, err := client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, 1, server.callCount())
}

func TestOwnerClientAsksAgainOnceTheEntryExpires(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newOwnerServer(accountID, userID)
	client, clock := ownerClient(t, server)

	_, err := client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	clock.Advance(time.Minute)
	_, err = client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	_, err = client.Owner(context.Background(), accountID)
	require.NoError(t, err)

	assert.Equal(t, 2, server.callCount())
}

func TestOwnerClientDoesNotCacheAnUnknownAccount(t *testing.T) {
	server := newOwnerServer()
	client, _ := ownerClient(t, server)
	accountID := uuid.New()

	for i := 0; i < 2; i++ {
		_, err := client.Owner(context.Background(), accountID)
		assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 2, server.callCount())
}

func TestOwnerClientReportsUnexpectedAnswersAsBadGateway(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"server error":    {http.StatusInternalServerError, `{"success":false}`},
		"other not found": {http.StatusNotFound, `{"success":false,"error":{"code":"NOT_FOUND","message":"route not found"}}`},
		"bare not found":  {http.StatusNotFound, `404 page not found`},
		"redirect":        {http.StatusFound, ``},
		"invalid json":    {http.StatusOK, `{"success":`},
		"invalid user id": {http.StatusOK, `{"success":true,"data":{"user_id":"nope"}}`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := &ownerServer{status: tc.status, body: tc.body}
			client, _ := ownerClient(t, server)
			accountID := uuid.New()

			for i := 0; i < 2; i++ {
				_, err := client.Owner(context.Background(), accountID)
				appErr := assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
				assert.Equal(t, "account service is unavailable", appErr.Message)
			}
			assert.Equal(t, 2, server.callCount())
		})
	}
}

func TestOwnerClientDoesNotFollowRedirects(t *testing.T) {
	target := &ownerServer{owners: map[string]uuid.UUID{}}
	targetServer := httptest.NewServer(target)
	defer targetServer.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()
	base, _ := url.Parse(redirecting.URL)

	_, err := owners.NewClient(base, time.Second, time.Minute).Owner(context.Background(), uuid.New())

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	assert.Equal(t, 0, target.callCount())
}

func TestOwnerClientReportsAnUnreachableService(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")

	_, err := owners.NewClient(base, time.Second, time.Minute).Owner(context.Background(), uuid.New())

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
}

func TestOwnerClientGivesUpAfterTheTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	base, _ := url.Parse(server.URL)

	start := time.Now()
	_, err := owners.NewClient(base, 50*time.Millisecond, time.Minute).Owner(context.Background(), uuid.New())

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestOwnerClientRejectsARequestItCannotBuild(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")
	var missing context.Context

	_, err := owners.NewClient(base, time.Second, time.Minute).Owner(missing, uuid.New())

	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
}

func TestOwnerClientEvictsExpiredEntriesFirstWhenFull(t *testing.T) {
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	server := newOwnerServer(first, uuid.New(), second, uuid.New(), third, uuid.New())
	client, clock := ownerClient(t, server)
	client.WithMaxEntries(2)

	lookup := func(accountID uuid.UUID) {
		_, err := client.Owner(context.Background(), accountID)
		require.NoError(t, err)
	}

	lookup(first)
	clock.Advance(30 * time.Second)
	lookup(second)
	clock.Advance(31 * time.Second)
	lookup(third)
	assert.Equal(t, 3, server.callCount())

	lookup(second)
	lookup(third)
	assert.Equal(t, 3, server.callCount())

	lookup(first)
	assert.Equal(t, 4, server.callCount())
}

func TestOwnerClientEvictsTheOldestEntryWhenFull(t *testing.T) {
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	server := newOwnerServer(first, uuid.New(), second, uuid.New(), third, uuid.New())
	client, clock := ownerClient(t, server)
	client.WithMaxEntries(2)

	lookup := func(accountID uuid.UUID) {
		_, err := client.Owner(context.Background(), accountID)
		require.NoError(t, err)
	}

	lookup(first)
	clock.Advance(time.Second)
	lookup(second)
	clock.Advance(time.Second)
	lookup(third)
	assert.Equal(t, 3, server.callCount())

	lookup(second)
	lookup(third)
	assert.Equal(t, 3, server.callCount())

	lookup(first)
	assert.Equal(t, 4, server.callCount())
}

func TestOwnerClientRefreshesAnEntryWithoutEvictingWhenFull(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	server := newOwnerServer(first, uuid.New(), second, uuid.New())
	client, clock := ownerClient(t, server)
	client.WithMaxEntries(2)

	lookup := func(accountID uuid.UUID) {
		_, err := client.Owner(context.Background(), accountID)
		require.NoError(t, err)
	}

	lookup(first)
	clock.Advance(30 * time.Second)
	lookup(second)
	clock.Advance(31 * time.Second)
	lookup(first)
	lookup(second)

	assert.Equal(t, 3, server.callCount())
}
