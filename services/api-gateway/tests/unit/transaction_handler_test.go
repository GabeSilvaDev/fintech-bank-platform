package unit

import (
	"net/http"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func transactionRouter(pub contracts.Publisher) http.Handler {
	h := handlers.NewTransactionHandler(pub)
	r := chi.NewRouter()
	r.Post("/transactions", h.Create)
	r.Post("/transfers", h.Transfer)
	return r
}

func TestCreateTransactionPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}
	accountID := tests.UUID()

	rec, body := call(transactionRouter(pub), http.MethodPost, "/transactions",
		`{"account_id":"`+accountID+`","type":"deposit","amount":150.5,"currency":"BRL","description":"salary","idempotency_key":"dep-1"}`)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	cmd := pub.Last()
	assert.Equal(t, events.Topics.TransactionCommands, cmd.Topic)
	assert.Equal(t, accountID, cmd.Key)
	assert.Equal(t, events.EventTypes.CreateTransaction, cmd.Event.Type)
	assert.Equal(t, "req-1", cmd.Event.TraceID)

	payload := cmd.Event.Payload.(events.CreateTransactionPayload)
	assert.Equal(t, accountID, payload.AccountID)
	assert.Equal(t, "deposit", payload.Type)
	assert.Equal(t, domain.AmountFromCents(15050), payload.Amount)
	assert.Equal(t, "BRL", payload.Currency)
	assert.Equal(t, "salary", payload.Description)
	assert.Equal(t, "dep-1", payload.IdempotencyKey)
	assert.Equal(t, cmd.Event.ID, body["data"].(map[string]interface{})["command_id"])
}

func TestCreateTransactionRejectsMalformedJSON(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(transactionRouter(pub), http.MethodPost, "/transactions", `[`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_JSON", errorCode(body))
}

func TestCreateTransactionValidatesFields(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(transactionRouter(pub), http.MethodPost, "/transactions",
		`{"account_id":"x","type":"refund","amount":0,"currency":"XYZ","idempotency_key":""}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	details := errorDetails(body)
	assert.Equal(t, "uuid", details["account_id"])
	assert.Equal(t, "oneof", details["type"])
	assert.Equal(t, "required", details["amount"])
	assert.Equal(t, "currency", details["currency"])
	assert.Equal(t, "required", details["idempotency_key"])
	assert.Empty(t, pub.Published)
}

func TestCreateTransactionValidatesIdempotencyKeyCharacters(t *testing.T) {
	pub := &tests.FakePublisher{}
	cases := []struct {
		name string
		key  string
	}{
		{"space", "abc def"},
		{"unicode", "café"},
		{"too_long", strings.Repeat("a", 65)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := call(transactionRouter(pub), http.MethodPost, "/transactions",
				`{"account_id":"`+tests.UUID()+`","type":"deposit","amount":10,"currency":"BRL","idempotency_key":"`+tc.key+`"}`)

			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Equal(t, "idempotency_key", errorDetails(body)["idempotency_key"])
		})
	}
	assert.Empty(t, pub.Published)
}

func TestCreateTransactionReturnsPublisherError(t *testing.T) {
	pub := &tests.FakePublisher{Err: apperrors.ServiceUnavailable("PUBLISH_FAILED", "down")}

	rec, _ := call(transactionRouter(pub), http.MethodPost, "/transactions",
		`{"account_id":"`+tests.UUID()+`","type":"withdrawal","amount":10,"currency":"BRL","idempotency_key":"w-1"}`)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTransferPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}
	from, to := tests.UUID(), tests.UUID()

	rec, _ := call(transactionRouter(pub), http.MethodPost, "/transfers",
		`{"from_account_id":"`+from+`","to_account_id":"`+to+`","amount":99.9,"currency":"brl","description":"rent","idempotency_key":"tr-1"}`)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	cmd := pub.Last()
	assert.Equal(t, events.Topics.TransactionCommands, cmd.Topic)
	assert.Equal(t, from, cmd.Key)
	assert.Equal(t, events.EventTypes.ProcessTransfer, cmd.Event.Type)

	payload := cmd.Event.Payload.(events.ProcessTransferPayload)
	assert.Equal(t, from, payload.FromAccountID)
	assert.Equal(t, to, payload.ToAccountID)
	assert.Equal(t, domain.AmountFromCents(9990), payload.Amount)
	assert.Equal(t, "BRL", payload.Currency)
	assert.Equal(t, "rent", payload.Description)
	assert.Equal(t, "tr-1", payload.IdempotencyKey)
}

func TestTransferRejectsMalformedJSON(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(transactionRouter(pub), http.MethodPost, "/transfers", `nope`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_JSON", errorCode(body))
}

func TestTransferRejectsSameAccount(t *testing.T) {
	pub := &tests.FakePublisher{}
	id := tests.UUID()

	rec, body := call(transactionRouter(pub), http.MethodPost, "/transfers",
		`{"from_account_id":"`+id+`","to_account_id":"`+id+`","amount":5,"currency":"BRL","idempotency_key":"tr-2"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "nefield", errorDetails(body)["to_account_id"])
	assert.Empty(t, pub.Published)
}

func TestTransferReturnsPublisherError(t *testing.T) {
	pub := &tests.FakePublisher{Err: apperrors.ServiceUnavailable("PUBLISH_FAILED", "down")}

	rec, _ := call(transactionRouter(pub), http.MethodPost, "/transfers",
		`{"from_account_id":"`+tests.UUID()+`","to_account_id":"`+tests.UUID()+`","amount":5,"currency":"BRL","idempotency_key":"tr-3"}`)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTransactionAmountsWithMoreThanTwoDecimalsAreRejected(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(transactionRouter(pub), http.MethodPost, "/transactions",
		`{"account_id":"`+tests.UUID()+`","type":"deposit","amount":10.005,"currency":"BRL","idempotency_key":"dep-2"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	assert.Equal(t, "amount", errorDetails(body)["amount"])

	rec, body = call(transactionRouter(pub), http.MethodPost, "/transfers",
		`{"from_account_id":"`+tests.UUID()+`","to_account_id":"`+tests.UUID()+`","amount":0.001,"currency":"BRL","idempotency_key":"tr-4"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "amount", errorDetails(body)["amount"])
	assert.Empty(t, pub.Published)
}
