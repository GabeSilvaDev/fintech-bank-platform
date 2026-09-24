package owners

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/google/uuid"
)

const (
	defaultMaxEntries         = 10000
	defaultMaxNegativeEntries = 1000
	maxResponseBytes          = 64 << 10
)

type entry struct {
	userID  uuid.UUID
	expires time.Time
}

type bounded struct {
	entries map[uuid.UUID]entry
	limit   int
}

func newBounded(limit int) *bounded {
	return &bounded{entries: make(map[uuid.UUID]entry), limit: limit}
}

func (b *bounded) get(id uuid.UUID, now time.Time) (entry, bool) {
	cached, ok := b.entries[id]
	return cached, ok && now.Before(cached.expires)
}

func (b *bounded) put(id uuid.UUID, cached entry, now time.Time) {
	if _, exists := b.entries[id]; !exists && len(b.entries) >= b.limit {
		b.evict(now)
	}
	b.entries[id] = cached
}

func (b *bounded) evict(now time.Time) {
	for id, cached := range b.entries {
		if !now.Before(cached.expires) {
			delete(b.entries, id)
		}
	}
	if len(b.entries) < b.limit {
		return
	}

	var oldestID uuid.UUID
	var oldest time.Time
	for id, cached := range b.entries {
		if oldest.IsZero() || cached.expires.Before(oldest) {
			oldestID, oldest = id, cached.expires
		}
	}
	delete(b.entries, oldestID)
}

type call struct {
	done   chan struct{}
	userID uuid.UUID
	err    error
}

type Client struct {
	base        *url.URL
	http        *http.Client
	ttl         time.Duration
	negativeTTL time.Duration
	now         func() time.Time
	mu          sync.Mutex
	found       *bounded
	missing     *bounded
	inflight    map[uuid.UUID]*call
}

func NewClient(base *url.URL, timeout, ttl time.Duration) *Client {
	return &Client{
		base: base,
		http: &http.Client{
			Transport: tracing.Transport(nil),
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		ttl:      ttl,
		now:      time.Now,
		found:    newBounded(defaultMaxEntries),
		missing:  newBounded(defaultMaxNegativeEntries),
		inflight: make(map[uuid.UUID]*call),
	}
}

func (c *Client) WithClock(now func() time.Time) *Client {
	c.now = now
	return c
}

func (c *Client) WithTransport(transport http.RoundTripper) *Client {
	c.http.Transport = tracing.Transport(transport)
	return c
}

func (c *Client) WithMaxEntries(maxEntries int) *Client {
	c.found.limit = maxEntries
	return c
}

func (c *Client) WithMaxNegativeEntries(maxEntries int) *Client {
	c.missing.limit = maxEntries
	return c
}

func (c *Client) WithNegativeTTL(ttl time.Duration) *Client {
	c.negativeTTL = ttl
	return c
}

func (c *Client) Owner(ctx context.Context, accountID uuid.UUID) (uuid.UUID, error) {
	c.mu.Lock()
	now := c.now()
	if cached, ok := c.found.get(accountID, now); ok {
		c.mu.Unlock()
		return cached.userID, nil
	}
	if _, ok := c.missing.get(accountID, now); ok {
		c.mu.Unlock()
		return uuid.Nil, contracts.ErrAccountNotFound
	}
	pending, ok := c.inflight[accountID]
	if !ok {
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return uuid.Nil, err
		}
		pending = &call{done: make(chan struct{})}
		c.inflight[accountID] = pending
		go c.resolve(context.WithoutCancel(ctx), accountID, pending)
	}
	c.mu.Unlock()

	select {
	case <-pending.done:
		return pending.userID, pending.err
	case <-ctx.Done():
		return uuid.Nil, ctx.Err()
	}
}

func (c *Client) resolve(ctx context.Context, accountID uuid.UUID, pending *call) {
	userID, err := c.lookup(ctx, accountID)

	c.mu.Lock()
	now := c.now()
	switch {
	case err == nil:
		delete(c.missing.entries, accountID)
		c.found.put(accountID, entry{userID: userID, expires: now.Add(c.ttl)}, now)
	case errors.Is(err, contracts.ErrAccountNotFound) && c.negativeTTL > 0:
		delete(c.found.entries, accountID)
		c.missing.put(accountID, entry{expires: now.Add(c.negativeTTL)}, now)
	}
	delete(c.inflight, accountID)
	pending.userID, pending.err = userID, err
	c.mu.Unlock()

	close(pending.done)
}

func (c *Client) lookup(ctx context.Context, accountID uuid.UUID) (userID uuid.UUID, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			userID, err = uuid.Nil, unavailable(fmt.Errorf("owner lookup panicked: %v", recovered))
		}
	}()
	return c.fetch(ctx, accountID)
}

const accountNotFoundCode = "ACCOUNT_NOT_FOUND"

type ownerEnvelope struct {
	Data struct {
		UserID string `json:"user_id"`
	} `json:"data"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func (c *Client) fetch(ctx context.Context, accountID uuid.UUID) (uuid.UUID, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.JoinPath("accounts", accountID.String(), "owner").String(), nil)
	if err != nil {
		return uuid.Nil, unavailable(err)
	}
	req.Header.Set(middleware.RequestIDHeader, middleware.GetRequestID(ctx))

	resp, err := c.http.Do(req)
	if err != nil {
		return uuid.Nil, unavailable(err)
	}
	defer drain(resp.Body)

	var payload ownerEnvelope
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&payload)

	if resp.StatusCode == http.StatusNotFound && payload.Error.Code == accountNotFoundCode {
		return uuid.Nil, contracts.ErrAccountNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return uuid.Nil, unavailable(fmt.Errorf("unexpected owner status %d (%q)", resp.StatusCode, payload.Error.Code))
	}
	if decodeErr != nil {
		return uuid.Nil, unavailable(decodeErr)
	}
	userID, err := uuid.Parse(payload.Data.UserID)
	if err != nil {
		return uuid.Nil, unavailable(fmt.Errorf("unexpected owner user_id %q: %w", payload.Data.UserID, err))
	}
	return userID, nil
}

func drain(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseBytes))
	_ = body.Close()
}

func unavailable(err error) error {
	return apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway).Wrap(err)
}
