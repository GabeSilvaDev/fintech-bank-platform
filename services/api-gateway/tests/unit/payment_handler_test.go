package unit

import (
	"net/http"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

const boletoCode = "34191790010100000012334567812309500000000000000"
const boletoCodeWithAmount = "34191790010100000012334567812309811000000015000"

func paymentRouter(pub contracts.Publisher) http.Handler {
	h := handlers.NewPaymentHandler(pub)
	r := chi.NewRouter()
	r.Post("/payments", h.Process)
	return r
}

func paymentBody(accountID, method, extra string) string {
	return `{"account_id":"` + accountID + `","payment_method":"` + method + `","amount":42.5,"currency":"BRL","recipient":"Loja X","description":"order 9","idempotency_key":"pay-1"` + extra + `}`
}

func TestProcessPixPaymentPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}
	accountID := tests.UUID()

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(accountID, "pix", `,"pix_key":"ana@example.com"`))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	cmd := pub.Last()
	assert.Equal(t, events.Topics.PaymentCommands, cmd.Topic)
	assert.Equal(t, accountID, cmd.Key)
	assert.Equal(t, events.EventTypes.ProcessPayment, cmd.Event.Type)
	assert.Equal(t, "req-1", cmd.Event.TraceID)

	payload := cmd.Event.Payload.(events.ProcessPaymentPayload)
	assert.Equal(t, accountID, payload.AccountID)
	assert.Equal(t, "pix", payload.PaymentMethod)
	assert.Equal(t, 42.5, payload.Amount)
	assert.Equal(t, "BRL", payload.Currency)
	assert.Equal(t, "Loja X", payload.Recipient)
	assert.Equal(t, "ana@example.com", payload.PixKey)
	assert.Equal(t, "order 9", payload.Description)
	assert.Equal(t, "pay-1", payload.IdempotencyKey)
	assert.Equal(t, cmd.Event.ID, body["data"].(map[string]interface{})["command_id"])
}

func TestProcessBoletoPaymentPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, _ := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "boleto", `,"boleto_code":"`+boletoCode+`"`))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, boletoCode, pub.Last().Event.Payload.(events.ProcessPaymentPayload).BoletoCode)
}

func TestProcessBoletoPaymentRejectsAmountMismatch(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments",
		`{"account_id":"`+tests.UUID()+`","payment_method":"boleto","amount":10,"currency":"BRL","recipient":"Energia SA","idempotency_key":"bol-1","boleto_code":"`+boletoCodeWithAmount+`"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	assert.Equal(t, "boleto_amount", errorDetails(body)["amount"])
	assert.Empty(t, pub.Published)
}

func TestProcessBoletoPaymentAcceptsMatchingAmount(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, _ := call(paymentRouter(pub), http.MethodPost, "/payments",
		`{"account_id":"`+tests.UUID()+`","payment_method":"boleto","amount":150,"currency":"BRL","recipient":"Energia SA","idempotency_key":"bol-2","boleto_code":"`+boletoCodeWithAmount+`"}`)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, boletoCodeWithAmount, pub.Last().Event.Payload.(events.ProcessPaymentPayload).BoletoCode)
}

func TestProcessTedPaymentPublishesCommand(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, _ := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "ted", `,"ted":{"bank_code":"341","branch":"0001","account":"123456","document":"52998224725"}`))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	payload := pub.Last().Event.Payload.(events.ProcessPaymentPayload)
	assert.Equal(t, "ted", payload.PaymentMethod)
	assert.Equal(t, &events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}, payload.TED)
}

func TestProcessPaymentRejectsMalformedJSON(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments", `{"amount":`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_JSON", errorCode(body))
}

func TestProcessPaymentValidatesFields(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments",
		`{"account_id":"x","payment_method":"cash","amount":-1,"currency":"BRL","recipient":"","idempotency_key":"k","pix_key":"!!","boleto_code":"ab"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	details := errorDetails(body)
	assert.Equal(t, "uuid", details["account_id"])
	assert.Equal(t, "oneof", details["payment_method"])
	assert.Equal(t, "gt", details["amount"])
	assert.Equal(t, "required", details["recipient"])
	assert.Equal(t, "pix_key", details["pix_key"])
	assert.Equal(t, "boleto", details["boleto_code"])
	assert.Empty(t, pub.Published)
}

func TestProcessPixPaymentRequiresPixKey(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "pix", ""))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	assert.Equal(t, "required", errorDetails(body)["pix_key"])
	assert.Empty(t, pub.Published)
}

func TestProcessBoletoPaymentRequiresBoletoCode(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "boleto", ""))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "required", errorDetails(body)["boleto_code"])
	assert.Empty(t, pub.Published)
}

func TestProcessPaymentRejectsWhitespaceRecipient(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "pix", `,"pix_key":"ana@example.com","recipient":"   "`))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(body))
	assert.Equal(t, "required", errorDetails(body)["recipient"])
	assert.Empty(t, pub.Published)
}

func TestProcessTedPaymentRejectsNonDigitBankCode(t *testing.T) {
	pub := &tests.FakePublisher{}

	rec, body := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "ted", `,"ted":{"bank_code":"-12","branch":"0001","account":"123456","document":"52998224725"}`))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "number", errorDetails(body)["bank_code"])
	assert.Empty(t, pub.Published)
}

func TestProcessPaymentReturnsPublisherError(t *testing.T) {
	pub := &tests.FakePublisher{Err: apperrors.ServiceUnavailable("PUBLISH_FAILED", "down")}

	rec, _ := call(paymentRouter(pub), http.MethodPost, "/payments", paymentBody(tests.UUID(), "ted", `,"ted":{"bank_code":"341","branch":"0001","account":"123456","document":"52998224725"}`))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
