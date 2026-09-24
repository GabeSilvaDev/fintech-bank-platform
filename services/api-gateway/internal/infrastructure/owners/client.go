package owners

import (
	"context"
	"encoding/json"
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
	expires time.Time
}

type Client struct {
	base       *url.URL
	http       *http.Client
	ttl        time.Duration
	now        func() time.Time
	maxEntries int
	mu         sync.Mutex
	cache      map[uuid.UUID]entry
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

func (c *Client) Owner(ctx context.Context, accountID uuid.UUID) (uuid.UUID, error) {
	if userID, ok := c.cached(accountID); ok {
		return userID, nil
	}

	userID, err := c.fetch(ctx, accountID)
	if err != nil {
		return uuid.Nil, err
	}

	c.store(accountID, userID)
	return userID, nil
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

func (c *Client) cached(accountID uuid.UUID) (uuid.UUID, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, ok := c.cache[accountID]
	if !ok || !c.now().Before(cached.expires) {
		return uuid.Nil, false
	}
	return cached.userID, true
}

func (c *Client) store(accountID, userID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if _, exists := c.cache[accountID]; !exists && len(c.cache) >= c.maxEntries {
		c.evict(now)
	}
	c.cache[accountID] = entry{userID: userID, expires: now.Add(c.ttl)}
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
