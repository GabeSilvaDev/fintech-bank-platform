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
	defaultMaxEntries = 10000
	maxResponseBytes  = 64 << 10
)

type entry struct {
	userID  uuid.UUID
	missing bool
	expires time.Time
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
	maxEntries  int
	mu          sync.Mutex
	cache       map[uuid.UUID]entry
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
		ttl:        ttl,
		now:        time.Now,
		maxEntries: defaultMaxEntries,
		cache:      make(map[uuid.UUID]entry),
		inflight:   make(map[uuid.UUID]*call),
	}
}

func (c *Client) WithClock(now func() time.Time) *Client {
	c.now = now
	return c
}

func (c *Client) WithMaxEntries(maxEntries int) *Client {
	c.maxEntries = maxEntries
	return c
}

func (c *Client) WithNegativeTTL(ttl time.Duration) *Client {
	c.negativeTTL = ttl
	return c
}

func (c *Client) Owner(ctx context.Context, accountID uuid.UUID) (uuid.UUID, error) {
	c.mu.Lock()
	now := c.now()
	if cached, ok := c.cache[accountID]; ok && now.Before(cached.expires) {
		c.mu.Unlock()
		if cached.missing {
			return uuid.Nil, contracts.ErrAccountNotFound
		}
		return cached.userID, nil
	}
	pending, ok := c.inflight[accountID]
	if !ok {
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
	userID, err := c.fetch(ctx, accountID)

	c.mu.Lock()
	switch {
	case err == nil:
		c.store(accountID, entry{userID: userID}, c.ttl)
	case errors.Is(err, contracts.ErrAccountNotFound) && c.negativeTTL > 0:
		c.store(accountID, entry{missing: true}, c.negativeTTL)
	}
	delete(c.inflight, accountID)
	pending.userID, pending.err = userID, err
	c.mu.Unlock()

	close(pending.done)
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

func (c *Client) store(accountID uuid.UUID, cached entry, ttl time.Duration) {
	now := c.now()
	if _, exists := c.cache[accountID]; !exists && len(c.cache) >= c.maxEntries {
		c.evict(now)
	}
	cached.expires = now.Add(ttl)
	c.cache[accountID] = cached
}

func (c *Client) evict(now time.Time) {
	for id, cached := range c.cache {
		if !now.Before(cached.expires) {
			delete(c.cache, id)
		}
	}
	if len(c.cache) < c.maxEntries {
		return
	}

	var oldestID uuid.UUID
	var oldest time.Time
	for id, cached := range c.cache {
		if oldest.IsZero() || cached.expires.Before(oldest) {
			oldestID, oldest = id, cached.expires
		}
	}
	delete(c.cache, oldestID)
}

func drain(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseBytes))
	_ = body.Close()
}

func unavailable(err error) error {
	return apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway).Wrap(err)
}
