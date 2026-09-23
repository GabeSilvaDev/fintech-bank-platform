package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

type harness struct {
	accounts  *tests.FakeAccountRepo
	customers *tests.FakeCustomerRepo
	numbers   []string
	service   *services.AccountService
}

func newHarness(numbers ...string) *harness {
	h := &harness{accounts: tests.NewFakeAccountRepo(), customers: tests.NewFakeCustomerRepo(), numbers: numbers}
	if len(h.numbers) == 0 {
		h.numbers = []string{"12345678"}
	}
	h.service = services.NewAccountService(h.accounts, h.customers, tests.FakeClock{T: now}, func() string {
		number := h.numbers[0]
		if len(h.numbers) > 1 {
			h.numbers = h.numbers[1:]
		}
		return number
	})
	return h
}

func (h *harness) activeAccount(balance int64) *models.Account {
	account := &models.Account{AccountID: uuid.New(), UserID: uuid.New(), Agency: models.Agency, Number: "00000001", Type: models.AccountTypeChecking, Status: models.AccountStatusActive, Currency: models.Currency, BalanceCents: balance, CreatedAt: now, UpdatedAt: now}
	h.accounts.Put(account)
	h.customers.Put(&models.Customer{UserID: account.UserID, Name: "Ana Souza", Email: "ana@example.com", Document: "52998224725", Phone: "11999887766", KYCStatus: models.KYCStatusPending, CreatedAt: now, UpdatedAt: now})
	return account
}

func validCreate() events.CreateAccountPayload {
	return events.CreateAccountPayload{UserID: uuid.NewString(), AccountType: "checking", Name: "Ana Souza", Email: "ana@example.com", Document: "529.982.247-25", Phone: "(11) 99988-7766"}
}

func TestCreateAccountCreatesCustomerAndAccount(t *testing.T) {
	h := newHarness()
	cmd := validCreate()

	created, err := h.service.Create(context.Background(), cmd)

	assert.NoError(t, err)
	assert.Equal(t, cmd.UserID, created.UserID)
	assert.Equal(t, "0001", created.Agency)
	assert.Equal(t, "12345678", created.AccountNumber)
	assert.Equal(t, "checking", created.AccountType)
	assert.Equal(t, "active", created.Status)
	assert.Equal(t, now, created.CreatedAt)

	account := h.accounts.Created[0]
	assert.Equal(t, created.AccountID, account.AccountID.String())
	assert.Equal(t, models.AccountStatusActive, account.Status)
	assert.Equal(t, "BRL", account.Currency)
	assert.Equal(t, int64(0), account.BalanceCents)
	assert.Equal(t, account.AccountID, h.accounts.Reserved["0001-12345678"])

	customer := h.customers.Upserts[0]
	assert.Equal(t, "52998224725", customer.Document)
	assert.Equal(t, "11999887766", customer.Phone)
	assert.Equal(t, models.KYCStatusPending, customer.KYCStatus)
	assert.Equal(t, now, customer.CreatedAt)
}

func TestCreateAccountKeepsExistingCustomerKYCAndCreatedAt(t *testing.T) {
	h := newHarness()
	cmd := validCreate()
	earlier := now.Add(-48 * time.Hour)
	h.customers.Put(&models.Customer{UserID: uuid.MustParse(cmd.UserID), Name: "Old", Email: "old@example.com", KYCStatus: "verified", CreatedAt: earlier, UpdatedAt: earlier})

	_, err := h.service.Create(context.Background(), cmd)

	assert.NoError(t, err)
	customer := h.customers.Customers[uuid.MustParse(cmd.UserID)]
	assert.Equal(t, "Ana Souza", customer.Name)
	assert.Equal(t, "verified", customer.KYCStatus)
	assert.Equal(t, earlier, customer.CreatedAt)
	assert.Equal(t, now, customer.UpdatedAt)
}

func TestCreateAccountRetriesOnNumberCollision(t *testing.T) {
	h := newHarness("11111111", "22222222")
	h.accounts.Reserved["0001-11111111"] = uuid.New()

	created, err := h.service.Create(context.Background(), validCreate())

	assert.NoError(t, err)
	assert.Equal(t, "22222222", created.AccountNumber)
}

func TestCreateAccountGivesUpAfterFiveCollisions(t *testing.T) {
	h := newHarness("11111111")
	h.accounts.Reserved["0001-11111111"] = uuid.New()

	_, err := h.service.Create(context.Background(), validCreate())

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Empty(t, h.accounts.Created)
}

func TestCreateAccountValidation(t *testing.T) {
	cases := map[string]func(*events.CreateAccountPayload){
		"invalid_user_id":      func(c *events.CreateAccountPayload) { c.UserID = "nope" },
		"invalid_account_type": func(c *events.CreateAccountPayload) { c.AccountType = "gold" },
		"invalid_name":         func(c *events.CreateAccountPayload) { c.Name = "A" },
		"invalid_email":        func(c *events.CreateAccountPayload) { c.Email = "bad" },
		"invalid_document":     func(c *events.CreateAccountPayload) { c.Document = "123" },
		"invalid_phone":        func(c *events.CreateAccountPayload) { c.Phone = "1" },
	}

	for code, mutate := range cases {
		h := newHarness()
		cmd := validCreate()
		mutate(&cmd)

		_, err := h.service.Create(context.Background(), cmd)

		assert.Equal(t, code, domain.InvalidCode(err), code)
		assert.Empty(t, h.accounts.Created, code)
		assert.Empty(t, h.customers.Upserts, code)
	}
}

func TestCreateAccountAcceptsCNPJAndNoPhone(t *testing.T) {
	h := newHarness()
	cmd := validCreate()
	cmd.Document = "11.222.333/0001-81"
	cmd.Phone = ""

	_, err := h.service.Create(context.Background(), cmd)

	assert.NoError(t, err)
	assert.Equal(t, "11222333000181", h.customers.Upserts[0].Document)
}

func TestCreateAccountPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	h.customers.Err = errors.New("db down")
	_, err := h.service.Create(context.Background(), validCreate())
	assert.EqualError(t, err, "db down")

	h = newHarness()
	h.accounts.Err = errors.New("db down")
	_, err = h.service.Create(context.Background(), validCreate())
	assert.EqualError(t, err, "db down")
}

func TestCreateAccountPropagatesAccountCreateError(t *testing.T) {
	h := newHarness()
	h.accounts.CreateErr = errors.New("insert failed")

	_, err := h.service.Create(context.Background(), validCreate())

	assert.EqualError(t, err, "insert failed")
}

func TestUpdateAccountRoutesProfileAndStatus(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	updated, err := h.service.Update(context.Background(), events.UpdateAccountPayload{AccountID: account.AccountID.String(), Name: tests.Ptr("Ana Lima"), Phone: tests.Ptr("(11) 98888-7777"), Status: tests.Ptr("blocked")})

	assert.NoError(t, err)
	assert.Equal(t, "Ana Lima", updated.Name)
	assert.Equal(t, "ana@example.com", updated.Email)
	assert.Equal(t, "11988887777", updated.Phone)
	assert.Equal(t, "blocked", updated.Status)
	assert.Equal(t, account.UserID.String(), updated.UserID)
	assert.Equal(t, now, updated.UpdatedAt)
	assert.Len(t, h.customers.Profiles, 1)
	assert.Equal(t, models.AccountStatusBlocked, h.accounts.Statuses[0].Status)
	assert.Nil(t, h.accounts.Statuses[0].ClosedAt)
}

func TestUpdateAccountStatusOnlySkipsProfile(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	_, err := h.service.Update(context.Background(), events.UpdateAccountPayload{AccountID: account.AccountID.String(), Status: tests.Ptr("active")})

	assert.NoError(t, err)
	assert.Empty(t, h.customers.Profiles)
	assert.Len(t, h.accounts.Statuses, 1)
}

func TestUpdateAccountRejections(t *testing.T) {
	h := newHarness()
	active := h.activeAccount(0)
	closed := h.activeAccount(0)
	closed.Status = models.AccountStatusClosed
	h.accounts.Put(closed)

	cases := map[string]events.UpdateAccountPayload{
		"invalid_account_id": {AccountID: "x", Name: tests.Ptr("Ana Lima")},
		"empty_update":       {AccountID: active.AccountID.String()},
		"account_closed":     {AccountID: closed.AccountID.String(), Name: tests.Ptr("Ana Lima")},
		"invalid_status":     {AccountID: active.AccountID.String(), Status: tests.Ptr("closed")},
		"invalid_email":      {AccountID: active.AccountID.String(), Email: tests.Ptr("bad")},
	}

	for code, cmd := range cases {
		_, err := h.service.Update(context.Background(), cmd)
		assert.Equal(t, code, domain.InvalidCode(err), code)
	}

	_, err := h.service.Update(context.Background(), events.UpdateAccountPayload{AccountID: uuid.NewString(), Name: tests.Ptr("Ana Lima")})
	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, h.customers.Profiles)
	assert.Empty(t, h.accounts.Statuses)
}

func TestUpdateAccountPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.customers.Err = errors.New("db down")

	_, err := h.service.Update(context.Background(), events.UpdateAccountPayload{AccountID: account.AccountID.String(), Name: tests.Ptr("Ana Lima")})

	assert.EqualError(t, err, "db down")
}

func TestUpdateAccountPropagatesStatusUpdateError(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.UpdateStatusErr = errors.New("status update failed")

	_, err := h.service.Update(context.Background(), events.UpdateAccountPayload{AccountID: account.AccountID.String(), Status: tests.Ptr("blocked")})

	assert.EqualError(t, err, "status update failed")
}

func TestUpdateAccountPropagatesCustomerGetError(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.customers.Err = errors.New("db down")

	_, err := h.service.Update(context.Background(), events.UpdateAccountPayload{AccountID: account.AccountID.String(), Status: tests.Ptr("active")})

	assert.EqualError(t, err, "db down")
}

func TestCloseAccount(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)

	deleted, err := h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: account.AccountID.String(), Reason: "customer request"})

	assert.NoError(t, err)
	assert.Equal(t, account.AccountID.String(), deleted.AccountID)
	assert.Equal(t, account.UserID.String(), deleted.UserID)
	assert.Equal(t, "customer request", deleted.Reason)
	assert.Equal(t, now, deleted.ClosedAt)
	change := h.accounts.Statuses[0]
	assert.Equal(t, models.AccountStatusClosed, change.Status)
	assert.Equal(t, now, *change.ClosedAt)
}

func TestClosePropagatesCloseError(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.CloseErr = errors.New("close failed")

	_, err := h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: account.AccountID.String()})

	assert.EqualError(t, err, "close failed")
	assert.Empty(t, h.accounts.Statuses)
}

func TestCloseRejectsWhenBalanceArrivesBeforeClosing(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.OnClose = func() { h.accounts.Accounts[account.AccountID].BalanceCents = 100 }

	_, err := h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: account.AccountID.String()})

	assert.Equal(t, "account_has_balance", domain.InvalidCode(err))
	assert.Equal(t, 1, h.accounts.CloseCalls)
	assert.Equal(t, models.AccountStatusActive, h.accounts.Accounts[account.AccountID].Status)
	assert.Empty(t, h.accounts.Statuses)
}

func TestCloseRejectsWhenClosedConcurrently(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.OnClose = func() { h.accounts.Accounts[account.AccountID].Status = models.AccountStatusClosed }

	_, err := h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: account.AccountID.String()})

	assert.Equal(t, "account_closed", domain.InvalidCode(err))
	assert.Equal(t, 1, h.accounts.CloseCalls)
	assert.Empty(t, h.accounts.Statuses)
}

func TestClosePropagatesRereadErrorAfterConflict(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.OnClose = func() { h.accounts.Accounts[account.AccountID].BalanceCents = 100 }
	h.accounts.GetErrs = []error{nil, errors.New("db down")}

	_, err := h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: account.AccountID.String()})

	assert.EqualError(t, err, "db down")
}

func TestCloseAccountRejections(t *testing.T) {
	h := newHarness()
	funded := h.activeAccount(500)
	closed := h.activeAccount(0)
	closed.Status = models.AccountStatusClosed
	h.accounts.Put(closed)

	_, err := h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: funded.AccountID.String()})
	assert.Equal(t, "account_has_balance", domain.InvalidCode(err))

	_, err = h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: closed.AccountID.String()})
	assert.Equal(t, "account_closed", domain.InvalidCode(err))

	_, err = h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: "x"})
	assert.Equal(t, "invalid_account_id", domain.InvalidCode(err))

	_, err = h.service.Close(context.Background(), events.DeleteAccountPayload{AccountID: uuid.NewString()})
	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, h.accounts.Statuses)
}

func TestGetAndListByUser(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)

	got, err := h.service.Get(context.Background(), account.AccountID)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), got.BalanceCents)

	list, err := h.service.ListByUser(context.Background(), account.UserID)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestOwnerReturnsAccountAndCustomer(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(100)

	got, customer, err := h.service.Owner(context.Background(), account.AccountID)

	assert.NoError(t, err)
	assert.Equal(t, account.AccountID, got.AccountID)
	assert.Equal(t, "Ana Souza", customer.Name)
	assert.Equal(t, "ana@example.com", customer.Email)
	assert.Equal(t, "11999887766", customer.Phone)
}

func TestOwnerUnknownAccount(t *testing.T) {
	h := newHarness()

	_, _, err := h.service.Owner(context.Background(), uuid.New())

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestOwnerMissingCustomer(t *testing.T) {
	h := newHarness()
	account := &models.Account{AccountID: uuid.New(), UserID: uuid.New(), Agency: models.Agency, Number: "00000002", Type: models.AccountTypeChecking, Status: models.AccountStatusActive, Currency: models.Currency, CreatedAt: now, UpdatedAt: now}
	h.accounts.Put(account)

	_, _, err := h.service.Owner(context.Background(), account.AccountID)

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestOwnerPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	account := h.activeAccount(0)
	h.accounts.Err = errors.New("db down")

	_, _, err := h.service.Owner(context.Background(), account.AccountID)
	assert.EqualError(t, err, "db down")

	h = newHarness()
	account = h.activeAccount(0)
	h.customers.Err = errors.New("db down")

	_, _, err = h.service.Owner(context.Background(), account.AccountID)
	assert.EqualError(t, err, "db down")
}

func TestRandomNumberHasEightDigits(t *testing.T) {
	assert.Regexp(t, `^\d{8}$`, services.RandomNumber())
}

func TestSystemClockReturnsCurrentTime(t *testing.T) {
	assert.WithinDuration(t, time.Now(), services.SystemClock{}.Now(), time.Second)
}
