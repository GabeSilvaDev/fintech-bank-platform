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
	"github.com/google/uuid"
)

type entry struct {
	contact models.Contact
	expires time.Time
}

type Client struct {
	baseURL string
	http    *http.Client
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	cache   map[uuid.UUID]entry
}

func NewClient(baseURL string, timeout, ttl time.Duration) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: timeout}, ttl: ttl, now: time.Now, cache: map[uuid.UUID]entry{}}
}

func (c *Client) WithClock(now func() time.Time) *Client {
	c.now = now
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
	c.cache[accountID] = entry{contact: contact, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()
	return contact, nil
}
