package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/stretchr/testify/suite"
)

type PaymentsTestSuite struct {
	tests.TestCase
}

func TestPaymentsSuite(t *testing.T) {
	suite.Run(t, new(PaymentsTestSuite))
}

func (s *PaymentsTestSuite) TestPixPaymentIsAccepted() {
	accountID := tests.UUID()

	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id":      accountID,
		"payment_method":  "pix",
		"amount":          80,
		"currency":        "BRL",
		"recipient":       "Mercado Y",
		"pix_key":         "11999887766",
		"idempotency_key": tests.RandomString(12),
	}).
		AssertAccepted().
		AssertSuccess().
		AssertJsonHas("data.command_id")

	s.Equal(events.Topics.PaymentCommands, s.Publisher.Last().Topic)
	s.Equal(accountID, s.Publisher.Last().Key)
	s.Equal(events.EventTypes.ProcessPayment, s.Publisher.Last().Event.Type)
}

func (s *PaymentsTestSuite) TestPixPaymentWithoutKeyIsRejected() {
	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id":      tests.UUID(),
		"payment_method":  "pix",
		"amount":          80,
		"currency":        "BRL",
		"recipient":       "Mercado Y",
		"idempotency_key": "p-1",
	}).
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.pix_key", "required")

	s.Empty(s.Publisher.Published)
}

func (s *PaymentsTestSuite) TestTedRequiresADestination() {
	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id": tests.UUID(), "payment_method": "ted", "amount": 10, "currency": "BRL",
		"recipient": "Ana Souza", "idempotency_key": "ted-1",
	}).AssertUnprocessableEntity().AssertJsonPath("error.details.ted", "required")
}

func (s *PaymentsTestSuite) TestTedDestinationIsValidated() {
	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id": tests.UUID(), "payment_method": "ted", "amount": 10, "currency": "BRL",
		"recipient": "Ana Souza", "idempotency_key": "ted-2",
		"ted": map[string]string{"bank_code": "34", "branch": "1", "account": "12", "document": "123"},
	}).AssertUnprocessableEntity().
		AssertJsonPath("error.details.bank_code", "len").
		AssertJsonPath("error.details.branch", "agency_number").
		AssertJsonPath("error.details.account", "account_number").
		AssertJsonPath("error.details.document", "cpf|cnpj")
}

func (s *PaymentsTestSuite) TestTedIsPublishedWithItsDestination() {
	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id": tests.UUID(), "payment_method": "ted", "amount": 10, "currency": "brl",
		"recipient": "Ana Souza", "idempotency_key": "ted-3",
		"ted": map[string]string{"bank_code": "341", "branch": "0001", "account": "123456", "document": "52998224725"},
	}).AssertAccepted()

	payload := s.Publisher.Last().Event.Payload.(events.ProcessPaymentPayload)
	s.Equal(&events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}, payload.TED)
}

func (s *PaymentsTestSuite) TestTedDestinationIsRejectedOnOtherMethods() {
	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id": tests.UUID(), "payment_method": "pix", "amount": 80, "currency": "BRL",
		"recipient": "Mercado Y", "pix_key": "11999887766", "idempotency_key": "ted-4",
		"ted": map[string]string{"bank_code": "341", "branch": "0001", "account": "123456", "document": "52998224725"},
	}).AssertUnprocessableEntity().AssertJsonPath("error.details.ted", "excluded")

	s.Empty(s.Publisher.Published)

	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id": tests.UUID(), "payment_method": "boleto", "amount": 150, "currency": "BRL",
		"recipient": "Energia SA", "idempotency_key": "ted-5",
		"boleto_code": "34191790010100000012334567812309811000000015000",
		"ted":         map[string]string{"bank_code": "34"},
	}).AssertUnprocessableEntity().AssertJsonPath("error.details.ted", "excluded")

	s.Empty(s.Publisher.Published)
}

func (s *PaymentsTestSuite) TestReadRoutesAreProxied() {
	s.Get("/api/v1/payments/00000000-0000-0000-0000-000000000000").AssertStatus(502).AssertErrorCode("UPSTREAM_UNAVAILABLE")
	s.Get("/api/v1/accounts/00000000-0000-0000-0000-000000000000/payments").AssertStatus(502).AssertErrorCode("UPSTREAM_UNAVAILABLE")
}

func (s *PaymentsTestSuite) TestBoletoCheckDigitsAreValidated() {
	s.Post("/api/v1/payments", map[string]interface{}{
		"account_id": tests.UUID(), "payment_method": "boleto", "amount": 150, "currency": "BRL",
		"recipient": "Energia SA", "idempotency_key": "bol-1",
		"boleto_code": "34191790020100000012334567812309811000000015000",
	}).AssertUnprocessableEntity().AssertJsonPath("error.details.boleto_code", "boleto")
}
