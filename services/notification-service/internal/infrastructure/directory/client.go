package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

const defaultMaxEntries = 10000

type entry struct {
	contact models.Contact
	expires time.Time
}

type Client struct {
	baseURL    string
	http       *http.Client
	ttl        time.Duration
	now        func() time.Time
	mu         sync.Mutex
	cache      map[uuid.UUID]entry
	maxEntries int
	group      singleflight.Group
}

func NewClient(baseURL string, timeout, ttl time.Duration) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: timeout, Transport: tracing.Transport(nil)}, ttl: ttl, now: time.Now, cache: map[uuid.UUID]entry{}, maxEntries: defaultMaxEntries}
}

func (c *Client) WithClock(now func() time.Time) *Client {
	c.now = now
	return c
}

func (c *Client) WithMaxEntries(maxEntries int) *Client {
	c.maxEntries = maxEntries
	return c
}

type ownerBody struct {
	Data struct {
		AccountID string `json:"account_id"`
		UserID    string `json:"user_id"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		Phone     string `json:"phone"`
	} `json:"data"`
}

func (c *Client) Lookup(ctx context.Context, accountID uuid.UUID) (models.Contact, error) {
	c.mu.Lock()
	cached, ok := c.cache[accountID]
	c.mu.Unlock()
	if ok && c.now().Before(cached.expires) {
		return cached.contact, nil
	}

	result, err, _ := c.group.Do(accountID.String(), func() (interface{}, error) {
		return c.fetch(ctx, accountID)
	})
	if err != nil {
		return models.Contact{}, err
	}
	return result.(models.Contact), nil
}

func (c *Client) fetch(ctx context.Context, accountID uuid.UUID) (models.Contact, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/accounts/"+accountID.String()+"/owner", nil)
	if err != nil {
		return models.Contact{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return models.Contact{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return models.Contact{}, domain.ErrNotFound
	default:
		return models.Contact{}, fmt.Errorf("account directory answered %d", resp.StatusCode)
	}

	var body ownerBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return models.Contact{}, err
	}
	userID, err := uuid.Parse(body.Data.UserID)
	if err != nil {
		return models.Contact{}, fmt.Errorf("account directory returned user_id %q", body.Data.UserID)
	}
	contact := models.Contact{AccountID: accountID, UserID: userID, Name: body.Data.Name, Email: body.Data.Email, Phone: body.Data.Phone}

	c.mu.Lock()
	c.store(accountID, contact)
	c.mu.Unlock()
	return contact, nil
}

func (c *Client) store(accountID uuid.UUID, contact models.Contact) {
	c.evict(accountID)
	c.cache[accountID] = entry{contact: contact, expires: c.now().Add(c.ttl)}
}

func (c *Client) evict(accountID uuid.UUID) {
	if _, exists := c.cache[accountID]; exists {
		return
	}
	if len(c.cache) < c.maxEntries {
		return
	}

	now := c.now()
	for id, e := range c.cache {
		if !now.Before(e.expires) {
			delete(c.cache, id)
		}
	}
	if len(c.cache) < c.maxEntries {
		return
	}

	var oldestID uuid.UUID
	var oldestExpires time.Time
	found := false
	for id, e := range c.cache {
		if !found || e.expires.Before(oldestExpires) {
			oldestID = id
			oldestExpires = e.expires
			found = true
		}
	}
	if found {
		delete(c.cache, oldestID)
	}
}
