package unit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

const (
	boleto150  = "34191790010100000012334567812309811000000015000"
	boletoOpen = "34191790010100000012334567812309500000000000000"
	boletoBad  = "34191790020100000012334567812309811000000015000"
)

type harness struct {
	repo    *tests.FakePaymentRepo
	gateway *tests.FakeGateway
	nextID  uuid.UUID
	service *services.PaymentService
}

func newHarness() *harness {
	h := &harness{repo: tests.NewFakePaymentRepo(), gateway: &tests.FakeGateway{}, nextID: uuid.New()}
	h.service = services.NewPaymentService(h.repo, h.gateway, tests.FakeClock{T: now}, func() uuid.UUID { return h.nextID })
	return h
}

func pix(accountID string) events.ProcessPaymentPayload {
	return events.ProcessPaymentPayload{AccountID: accountID, PaymentMethod: "pix", Amount: 42.5, Currency: "brl", Recipient: "  Ana Souza  ", PixKey: "ana@example.com", Description: "lunch", IdempotencyKey: " pay-1 "}
}

func boleto(accountID, code string, amount float64) events.ProcessPaymentPayload {
	return events.ProcessPaymentPayload{AccountID: accountID, PaymentMethod: "boleto", Amount: amount, Currency: "BRL", Recipient: "Energia SA", BoletoCode: code, IdempotencyKey: "bol-1"}
}

func ted(accountID string) events.ProcessPaymentPayload {
	return events.ProcessPaymentPayload{AccountID: accountID, PaymentMethod: "ted", Amount: 1000, Currency: "BRL", Recipient: "Bruno Lima", IdempotencyKey: "ted-1", TED: &events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}}
}

func TestCreatePixRecordsAndRequestsDebit(t *testing.T) {
	h := newHarness()
	account := uuid.New()

	res, err := h.service.Create(context.Background(), pix(account.String()), "trace-1")

	assert.NoError(t, err)
	payment := h.repo.Created[0]
	assert.Equal(t, h.nextID, payment.ID)
	assert.Equal(t, account, payment.AccountID)
	assert.Equal(t, models.MethodPix, payment.Method)
	assert.Equal(t, models.StatusPending, payment.Status)
	assert.Equal(t, int64(4250), payment.AmountCents)
	assert.Equal(t, "BRL", payment.Currency)
	assert.Equal(t, "Ana Souza", payment.Recipient)
	assert.Equal(t, "ana@example.com", payment.PixKey)
	assert.Equal(t, "lunch", payment.Description)
	assert.Equal(t, "pay-1", payment.IdempotencyKey)
	assert.Equal(t, now, payment.CreatedAt)
	assert.Equal(t, h.nextID, h.repo.Keys[tests.KeyOf(account, "pay-1")])

	assert.Len(t, res.Messages, 2)
	created := res.Messages[0]
	assert.Equal(t, events.Topics.PaymentEvents, created.Topic)
	assert.Equal(t, account.String(), created.Key)
	assert.Equal(t, events.EventTypes.PaymentCreated, created.Event.Type)
	assert.Equal(t, "payment-service", created.Event.Source)
	assert.Equal(t, "trace-1", created.Event.TraceID)
	createdPayload := created.Event.Payload.(events.PaymentCreatedPayload)
	assert.Equal(t, h.nextID.String(), createdPayload.PaymentID)
	assert.Equal(t, "pix", createdPayload.PaymentMethod)
	assert.Equal(t, 42.5, createdPayload.Amount)
	assert.Equal(t, "Ana Souza", createdPayload.Recipient)
	assert.Equal(t, "pay-1", createdPayload.IdempotencyKey)
	assert.Equal(t, now, createdPayload.CreatedAt)

	debit := res.Messages[1]
	assert.Equal(t, events.Topics.AccountCommands, debit.Topic)
	assert.Equal(t, account.String(), debit.Key)
	assert.Equal(t, events.EventTypes.DebitAccount, debit.Event.Type)
	assert.Equal(t, "payment-service", debit.Event.Source)
	assert.Equal(t, "trace-1", debit.Event.TraceID)
	debitPayload := debit.Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, account.String(), debitPayload.AccountID)
	assert.Equal(t, 42.5, debitPayload.Amount)
	assert.Equal(t, "BRL", debitPayload.Currency)
	assert.Equal(t, "payment:"+h.nextID.String(), debitPayload.Reference)
	assert.Equal(t, "payment:"+h.nextID.String()+":debit", debitPayload.IdempotencyKey)
}

func TestCreateBoletoAndTed(t *testing.T) {
	h := newHarness()
	_, err := h.service.Create(context.Background(), boleto(uuid.NewString(), boleto150, 150), "t")
	assert.NoError(t, err)
	assert.Equal(t, boleto150, h.repo.Created[0].BoletoCode)
	assert.Nil(t, h.repo.Created[0].TED)

	h = newHarness()
	_, err = h.service.Create(context.Background(), boleto(uuid.NewString(), boletoOpen, 99.99), "t")
	assert.NoError(t, err)
	assert.Equal(t, int64(9999), h.repo.Created[0].AmountCents)

	h = newHarness()
	_, err = h.service.Create(context.Background(), ted(uuid.NewString()), "t")
	assert.NoError(t, err)
	assert.Equal(t, &models.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}, h.repo.Created[0].TED)

	h = newHarness()
	cnpj := ted(uuid.NewString())
	cnpj.TED.Document = "11222333000181"
	_, err = h.service.Create(context.Background(), cnpj, "t")
	assert.NoError(t, err)
}

func TestCreateValidation(t *testing.T) {
	account := uuid.NewString()
	type testCase struct {
		code string
		cmd  events.ProcessPaymentPayload
	}
	build := func(code string, base events.ProcessPaymentPayload, mutate func(*events.ProcessPaymentPayload)) testCase {
		mutate(&base)
		return testCase{code: code, cmd: base}
	}
	cases := []testCase{
		build("invalid_account_id", pix(account), func(c *events.ProcessPaymentPayload) { c.AccountID = "x" }),
		build("invalid_payment_method", pix(account), func(c *events.ProcessPaymentPayload) { c.PaymentMethod = "doc" }),
		build("invalid_amount", pix(account), func(c *events.ProcessPaymentPayload) { c.Amount = -1 }),
		build("unsupported_currency", pix(account), func(c *events.ProcessPaymentPayload) { c.Currency = "USD" }),
		build("invalid_recipient", pix(account), func(c *events.ProcessPaymentPayload) { c.Recipient = "   " }),
		build("invalid_recipient", pix(account), func(c *events.ProcessPaymentPayload) { c.Recipient = strings.Repeat("é", 121) }),
		build("invalid_idempotency_key", pix(account), func(c *events.ProcessPaymentPayload) { c.IdempotencyKey = strings.Repeat("k", 65) }),
		build("invalid_description", pix(account), func(c *events.ProcessPaymentPayload) { c.Description = strings.Repeat("d", 256) }),
		build("invalid_pix_key", pix(account), func(c *events.ProcessPaymentPayload) { c.PixKey = "not a key" }),
		build("invalid_boleto", boleto(account, boletoBad, 150), func(c *events.ProcessPaymentPayload) {}),
		build("boleto_amount_mismatch", boleto(account, boleto150, 149.99), func(c *events.ProcessPaymentPayload) {}),
		build("invalid_ted_destination", ted(account), func(c *events.ProcessPaymentPayload) { c.TED = nil }),
		build("invalid_ted_destination", ted(account), func(c *events.ProcessPaymentPayload) { c.TED.BankCode = "34a" }),
		build("invalid_ted_destination", ted(account), func(c *events.ProcessPaymentPayload) { c.TED.BankCode = "3411" }),
		build("invalid_ted_destination", ted(account), func(c *events.ProcessPaymentPayload) { c.TED.Branch = "1" }),
		build("invalid_ted_destination", ted(account), func(c *events.ProcessPaymentPayload) { c.TED.Account = "12" }),
		build("invalid_ted_destination", ted(account), func(c *events.ProcessPaymentPayload) { c.TED.Document = "12345678900" }),
	}

	for i, c := range cases {
		h := newHarness()
		_, err := h.service.Create(context.Background(), c.cmd, "t")
		assert.Equal(t, c.code, domain.InvalidCode(err), i)
		assert.Empty(t, h.repo.Created, i)
		assert.Empty(t, h.repo.Keys, i)
	}
}

func TestCreateDuplicateKeyIsNoOpAndIsScopedByAccount(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	existing := &models.Payment{ID: uuid.New(), AccountID: account, Status: models.StatusCompleted}
	h.repo.Put(existing)
	h.repo.Keys[tests.KeyOf(account, "pay-1")] = existing.ID

	res, err := h.service.Create(context.Background(), pix(account.String()), "t")
	assert.ErrorIs(t, err, models.ErrDuplicateKey)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.repo.Created)

	_, err = h.service.Create(context.Background(), pix(uuid.NewString()), "t")
	assert.NoError(t, err)
	assert.Len(t, h.repo.Created, 1)
}

func TestCreateRecoversAReservedKeyWithoutARow(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	h.repo.CreateErr = errors.New("write timeout")
	_, err := h.service.Create(context.Background(), pix(account.String()), "t")
	assert.EqualError(t, err, "write timeout")

	h.repo.CreateErr = nil
	first := h.nextID
	h.nextID = uuid.New()
	res, err := h.service.Create(context.Background(), pix(account.String()), "t")

	assert.NoError(t, err)
	assert.Equal(t, first, h.repo.Created[0].ID)
	assert.Equal(t, first.String(), res.Messages[0].Event.Payload.(events.PaymentCreatedPayload).PaymentID)
}

func TestCreatePropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	h.repo.ReserveErr = errors.New("db down")
	_, err := h.service.Create(context.Background(), pix(uuid.NewString()), "t")
	assert.EqualError(t, err, "db down")

	h = newHarness()
	account := uuid.New()
	h.repo.Keys[tests.KeyOf(account, "pay-1")] = uuid.New()
	h.repo.GetErrs = []error{errors.New("db down")}
	_, err = h.service.Create(context.Background(), pix(account.String()), "t")
	assert.EqualError(t, err, "db down")
}

func TestGetAndListByAccount(t *testing.T) {
	h := newHarness()
	account := uuid.New()
	_, _ = h.service.Create(context.Background(), pix(account.String()), "t")

	payment, err := h.service.Get(context.Background(), h.nextID)
	assert.NoError(t, err)
	assert.Equal(t, account, payment.AccountID)

	list, err := h.service.ListByAccount(context.Background(), account, 10)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestSystemClock(t *testing.T) {
	assert.WithinDuration(t, time.Now(), services.SystemClock{}.Now(), time.Second)
}
