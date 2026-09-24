package unit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	base := &url.URL{Scheme: "http", Host: "bad host"}

	_, err := owners.NewClient(base, time.Second, time.Minute).Owner(context.Background(), uuid.New())

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

type blockingOwnerServer struct {
	mu        sync.Mutex
	calls     int
	status    int
	body      string
	entered   chan struct{}
	release   chan struct{}
	cancelled []bool
	requestID string
}

func newBlockingOwnerServer(status int, body string) *blockingOwnerServer {
	return &blockingOwnerServer{
		status:  status,
		body:    body,
		entered: make(chan struct{}, 16),
		release: make(chan struct{}),
	}
}

func (s *blockingOwnerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls++
	s.requestID = r.Header.Get(middleware.RequestIDHeader)
	s.mu.Unlock()
	s.entered <- struct{}{}
	<-s.release

	s.mu.Lock()
	s.cancelled = append(s.cancelled, r.Context().Err() != nil)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	_, _ = w.Write([]byte(s.body))
}

func (s *blockingOwnerServer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *blockingOwnerServer) cancelledRequests() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bool(nil), s.cancelled...)
}

type signallingClock struct {
	fakeClock
	ticks chan struct{}
}

func (c *signallingClock) Now() time.Time {
	select {
	case c.ticks <- struct{}{}:
	default:
	}
	return c.fakeClock.Now()
}

func (c *signallingClock) awaitLookups(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		<-c.ticks
	}
}

func blockingOwnerClient(t *testing.T, server *blockingOwnerServer) (*owners.Client, *signallingClock) {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	base, err := url.Parse(httpServer.URL)
	require.NoError(t, err)
	clock := &signallingClock{
		fakeClock: fakeClock{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)},
		ticks:     make(chan struct{}, 64),
	}
	return owners.NewClient(base, 5*time.Second, time.Minute).WithClock(clock.Now), clock
}

type ownerResult struct {
	userID uuid.UUID
	err    error
}

func lookupAsync(client *owners.Client, ctx context.Context, accountID uuid.UUID) <-chan ownerResult {
	result := make(chan ownerResult, 1)
	go func() {
		userID, err := client.Owner(ctx, accountID)
		result <- ownerResult{userID: userID, err: err}
	}()
	return result
}

func ownerBody(accountID, userID uuid.UUID) string {
	return `{"success":true,"data":{"account_id":"` + accountID.String() + `","user_id":"` + userID.String() + `"}}`
}

const notFoundBody = `{"success":false,"error":{"code":"ACCOUNT_NOT_FOUND","message":"account not found"}}`

func TestOwnerClientSharesOneLookupBetweenConcurrentCallers(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newBlockingOwnerServer(http.StatusOK, ownerBody(accountID, userID))
	client, clock := blockingOwnerClient(t, server)

	const callers = 10
	results := make([]<-chan ownerResult, callers)
	for i := range results {
		results[i] = lookupAsync(client, context.Background(), accountID)
	}
	clock.awaitLookups(t, callers)
	close(server.release)

	for _, result := range results {
		got := <-result
		require.NoError(t, got.err)
		assert.Equal(t, userID, got.userID)
	}
	assert.Equal(t, 1, server.callCount())
}

func TestOwnerClientSharesAnUnknownAccountBetweenConcurrentCallers(t *testing.T) {
	server := newBlockingOwnerServer(http.StatusNotFound, notFoundBody)
	client, clock := blockingOwnerClient(t, server)
	accountID := uuid.New()

	const callers = 5
	results := make([]<-chan ownerResult, callers)
	for i := range results {
		results[i] = lookupAsync(client, context.Background(), accountID)
	}
	clock.awaitLookups(t, callers)
	close(server.release)

	for _, result := range results {
		got := <-result
		assert.ErrorIs(t, got.err, contracts.ErrAccountNotFound)
		assert.Equal(t, uuid.Nil, got.userID)
	}
	assert.Equal(t, 1, server.callCount())
}

func TestOwnerClientSharesAFailureButDoesNotKeepIt(t *testing.T) {
	server := newBlockingOwnerServer(http.StatusInternalServerError, `{"success":false}`)
	client, clock := blockingOwnerClient(t, server)
	client.WithNegativeTTL(time.Minute)
	accountID := uuid.New()

	first := lookupAsync(client, context.Background(), accountID)
	second := lookupAsync(client, context.Background(), accountID)
	clock.awaitLookups(t, 2)
	close(server.release)

	for _, result := range []<-chan ownerResult{first, second} {
		got := <-result
		assertAppError(t, got.err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	}
	assert.Equal(t, 1, server.callCount())

	_, err := client.Owner(context.Background(), accountID)
	assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	assert.Equal(t, 2, server.callCount())
}

func TestOwnerClientLetsACancelledWaiterGoWhileOthersGetTheOwner(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newBlockingOwnerServer(http.StatusOK, ownerBody(accountID, userID))
	client, clock := blockingOwnerClient(t, server)

	leader := lookupAsync(client, context.Background(), accountID)
	<-server.entered
	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	waiter := lookupAsync(client, waiterCtx, accountID)
	other := lookupAsync(client, context.Background(), accountID)
	clock.awaitLookups(t, 3)

	cancelWaiter()
	cancelled := <-waiter
	assert.ErrorIs(t, cancelled.err, context.Canceled)
	assert.Equal(t, uuid.Nil, cancelled.userID)

	close(server.release)
	for _, result := range []<-chan ownerResult{leader, other} {
		got := <-result
		require.NoError(t, got.err)
		assert.Equal(t, userID, got.userID)
	}
	assert.Equal(t, 1, server.callCount())
}

func TestOwnerClientKeepsTheSharedLookupAliveWhenItsStarterGivesUp(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newBlockingOwnerServer(http.StatusOK, ownerBody(accountID, userID))
	client, clock := blockingOwnerClient(t, server)

	leaderCtx, cancelLeader := context.WithCancel(requestContext("req-leader"))
	leader := lookupAsync(client, leaderCtx, accountID)
	<-server.entered
	follower := lookupAsync(client, context.Background(), accountID)
	clock.awaitLookups(t, 2)

	cancelLeader()
	gaveUp := <-leader
	assert.ErrorIs(t, gaveUp.err, context.Canceled)

	close(server.release)
	got := <-follower
	require.NoError(t, got.err)
	assert.Equal(t, userID, got.userID)
	assert.Equal(t, []bool{false}, server.cancelledRequests())
	assert.Equal(t, 1, server.callCount())
	assert.Equal(t, "req-leader", server.requestID)

	cached, err := client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, cached)
	assert.Equal(t, 1, server.callCount())
}

func TestOwnerClientReturnsTheCallersErrorWhenAlreadyCancelled(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newBlockingOwnerServer(http.StatusOK, ownerBody(accountID, userID))
	client, _ := blockingOwnerClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Owner(ctx, accountID)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, server.callCount())

	close(server.release)
	got, err := client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 1, server.callCount())

	got, err = client.Owner(ctx, accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 1, server.callCount())
}

func TestOwnerClientAnswersACancelledCallerFromRememberedUnknownAccounts(t *testing.T) {
	server := newOwnerServer()
	client, _ := ownerClient(t, server)
	client.WithNegativeTTL(5 * time.Second)
	accountID := uuid.New()
	_, err := client.Owner(context.Background(), accountID)
	require.ErrorIs(t, err, contracts.ErrAccountNotFound)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.Owner(ctx, accountID)

	assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	_, err = client.Owner(ctx, uuid.New())
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, server.callCount())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func memoryOwnerClient(known map[uuid.UUID]uuid.UUID, calls *int, panics func(int) bool) (*owners.Client, *fakeClock) {
	var mu sync.Mutex
	clock := &fakeClock{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
	base, _ := url.Parse("http://accounts.test")
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		*calls++
		call := *calls
		mu.Unlock()
		if panics != nil && panics(call) {
			panic("transport exploded")
		}
		for accountID, userID := range known {
			if r.URL.Path == "/accounts/"+accountID.String()+"/owner" {
				return memoryResponse(http.StatusOK, ownerBody(accountID, userID)), nil
			}
		}
		return memoryResponse(http.StatusNotFound, notFoundBody), nil
	})
	return owners.NewClient(base, time.Second, time.Minute).WithClock(clock.Now).WithTransport(transport), clock
}

func memoryResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestOwnerClientTurnsAPanickingLookupIntoBadGateway(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	calls := 0
	client, _ := memoryOwnerClient(map[uuid.UUID]uuid.UUID{accountID: userID}, &calls, func(call int) bool { return call == 1 })
	client.WithNegativeTTL(time.Minute)

	got, err := client.Owner(context.Background(), accountID)

	assert.Equal(t, uuid.Nil, got)
	appErr := assertAppError(t, err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	assert.Equal(t, "account service is unavailable", appErr.Message)
	got, err = client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 2, calls)
}

func TestOwnerClientReleasesEveryWaiterWhenTheSharedLookupPanics(t *testing.T) {
	accountID := uuid.New()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	base, _ := url.Parse("http://accounts.test")
	clock := &signallingClock{fakeClock: fakeClock{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}, ticks: make(chan struct{}, 64)}
	client := owners.NewClient(base, 5*time.Second, time.Minute).WithClock(clock.Now).WithNegativeTTL(time.Minute).WithTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			entered <- struct{}{}
			<-release
			panic("transport exploded")
		}
		return memoryResponse(http.StatusNotFound, notFoundBody), nil
	}))

	leader := lookupAsync(client, context.Background(), accountID)
	<-entered
	follower := lookupAsync(client, context.Background(), accountID)
	clock.awaitLookups(t, 2)
	close(release)

	for _, result := range []<-chan ownerResult{leader, follower} {
		got := <-result
		assertAppError(t, got.err, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	}
	_, err := client.Owner(context.Background(), accountID)
	assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, calls)
}

func TestOwnerClientRemembersAThousandUnknownAccountsByDefault(t *testing.T) {
	calls := 0
	client, clock := memoryOwnerClient(nil, &calls, nil)
	client.WithNegativeTTL(time.Minute)

	unknown := make([]uuid.UUID, 1001)
	for i := range unknown {
		unknown[i] = uuid.New()
		clock.Advance(time.Millisecond)
		_, err := client.Owner(context.Background(), unknown[i])
		require.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	require.Equal(t, 1001, calls)

	for _, accountID := range unknown[1:] {
		_, err := client.Owner(context.Background(), accountID)
		require.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 1001, calls)
	_, err := client.Owner(context.Background(), unknown[0])
	assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	assert.Equal(t, 1002, calls)
}

func TestOwnerClientNeverEvictsKnownOwnersForUnknownAccounts(t *testing.T) {
	known, userID := uuid.New(), uuid.New()
	calls := 0
	client, clock := memoryOwnerClient(map[uuid.UUID]uuid.UUID{known: userID}, &calls, nil)
	client.WithNegativeTTL(5 * time.Second).WithMaxEntries(1).WithMaxNegativeEntries(1)

	_, err := client.Owner(context.Background(), known)
	require.NoError(t, err)
	clock.Advance(58 * time.Second)
	for i := 0; i < 3; i++ {
		clock.Advance(time.Millisecond)
		_, err := client.Owner(context.Background(), uuid.New())
		require.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 4, calls)

	got, err := client.Owner(context.Background(), known)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 4, calls)
}

func TestOwnerClientRemembersAnUnknownAccountBriefly(t *testing.T) {
	server := newOwnerServer()
	client, clock := ownerClient(t, server)
	client.WithNegativeTTL(5 * time.Second)
	accountID := uuid.New()

	for i := 0; i < 3; i++ {
		_, err := client.Owner(context.Background(), accountID)
		assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 1, server.callCount())

	clock.Advance(4 * time.Second)
	_, err := client.Owner(context.Background(), accountID)
	assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	assert.Equal(t, 1, server.callCount())

	userID := uuid.New()
	server.mu.Lock()
	server.owners[accountID.String()] = userID
	server.mu.Unlock()
	clock.Advance(time.Second)

	got, err := client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 2, server.callCount())

	clock.Advance(30 * time.Second)
	got, err = client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 2, server.callCount())
}

func TestOwnerClientForgetsAnAccountThatDisappearsAfterItsOwnerExpires(t *testing.T) {
	accountID, userID := uuid.New(), uuid.New()
	server := newOwnerServer(accountID, userID)
	client, clock := ownerClient(t, server)
	client.WithNegativeTTL(5 * time.Second)

	got, err := client.Owner(context.Background(), accountID)
	require.NoError(t, err)
	assert.Equal(t, userID, got)

	server.mu.Lock()
	delete(server.owners, accountID.String())
	server.mu.Unlock()
	clock.Advance(time.Minute)

	for i := 0; i < 2; i++ {
		_, err = client.Owner(context.Background(), accountID)
		assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 2, server.callCount())
}

func TestOwnerClientKeepsUnknownAccountsWithinTheirOwnBound(t *testing.T) {
	known, userID := uuid.New(), uuid.New()
	server := newOwnerServer(known, userID)
	client, clock := ownerClient(t, server)
	client.WithNegativeTTL(5 * time.Second).WithMaxNegativeEntries(2)

	_, err := client.Owner(context.Background(), known)
	require.NoError(t, err)

	unknown := make([]uuid.UUID, 5)
	for i := range unknown {
		unknown[i] = uuid.New()
		clock.Advance(time.Millisecond)
		_, err := client.Owner(context.Background(), unknown[i])
		assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 6, server.callCount())

	for _, accountID := range unknown[3:] {
		_, err = client.Owner(context.Background(), accountID)
		assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	got, err := client.Owner(context.Background(), known)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
	assert.Equal(t, 6, server.callCount())

	_, err = client.Owner(context.Background(), unknown[2])
	assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	assert.Equal(t, 7, server.callCount())
}

func TestOwnerClientDoesNotRememberUnknownAccountsWithANegativeTTLOfZero(t *testing.T) {
	server := newOwnerServer()
	client, _ := ownerClient(t, server)
	client.WithNegativeTTL(0)
	accountID := uuid.New()

	for i := 0; i < 3; i++ {
		_, err := client.Owner(context.Background(), accountID)
		assert.ErrorIs(t, err, contracts.ErrAccountNotFound)
	}
	assert.Equal(t, 3, server.callCount())
}
