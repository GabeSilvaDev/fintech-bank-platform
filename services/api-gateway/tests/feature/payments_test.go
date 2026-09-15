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
