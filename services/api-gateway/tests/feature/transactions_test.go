package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/stretchr/testify/suite"
)

type TransactionsTestSuite struct {
	tests.TestCase
}

func TestTransactionsSuite(t *testing.T) {
	suite.Run(t, new(TransactionsTestSuite))
}

func (s *TransactionsTestSuite) TestCreateTransactionIsAccepted() {
	accountID := tests.UUID()

	s.Post("/api/v1/transactions", map[string]interface{}{
		"account_id":      accountID,
		"type":            "deposit",
		"amount":          250,
		"currency":        "BRL",
		"idempotency_key": tests.RandomString(12),
	}).
		AssertAccepted().
		AssertSuccess().
		AssertJsonHas("data.command_id")

	s.Equal(events.Topics.TransactionCommands, s.Publisher.Last().Topic)
	s.Equal(accountID, s.Publisher.Last().Key)
	s.Equal(events.EventTypes.CreateTransaction, s.Publisher.Last().Event.Type)
}

func (s *TransactionsTestSuite) TestCreateTransactionValidationError() {
	s.Post("/api/v1/transactions", map[string]interface{}{"amount": 0}).
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.account_id", "required")
}

func (s *TransactionsTestSuite) TestTransferIsAccepted() {
	from := tests.UUID()

	s.Post("/api/v1/transfers", map[string]interface{}{
		"from_account_id": from,
		"to_account_id":   tests.UUID(),
		"amount":          10.5,
		"currency":        "BRL",
		"idempotency_key": tests.RandomString(12),
	}).
		AssertAccepted().
		AssertSuccess()

	s.Equal(from, s.Publisher.Last().Key)
	s.Equal(events.EventTypes.ProcessTransfer, s.Publisher.Last().Event.Type)
}

func (s *TransactionsTestSuite) TestReadRoutesAreProxied() {
	s.Get("/api/v1/transactions/00000000-0000-0000-0000-000000000000").AssertStatus(502).AssertErrorCode("UPSTREAM_UNAVAILABLE")
	s.Get("/api/v1/accounts/00000000-0000-0000-0000-000000000000/transactions").AssertStatus(502).AssertErrorCode("UPSTREAM_UNAVAILABLE")
}

func (s *TransactionsTestSuite) TestTransferToSameAccountIsRejected() {
	id := tests.UUID()

	s.Post("/api/v1/transfers", map[string]interface{}{
		"from_account_id": id,
		"to_account_id":   id,
		"amount":          1,
		"currency":        "BRL",
		"idempotency_key": "same",
	}).
		AssertUnprocessableEntity().
		AssertJsonPath("error.details.to_account_id", "nefield")

	s.Empty(s.Publisher.Published)
}
