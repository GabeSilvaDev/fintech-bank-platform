package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/stretchr/testify/suite"
)

type AccountsTestSuite struct {
	tests.TestCase
}

func TestAccountsSuite(t *testing.T) {
	suite.Run(t, new(AccountsTestSuite))
}

func (s *AccountsTestSuite) validAccount() map[string]interface{} {
	return map[string]interface{}{
		"user_id":      tests.UUID(),
		"account_type": "savings",
		"name":         "Bruno Costa",
		"email":        tests.RandomEmail(),
		"document":     "52998224725",
	}
}

func (s *AccountsTestSuite) TestCreateAccountIsAccepted() {
	s.Post("/api/v1/accounts", s.validAccount()).
		AssertAccepted().
		AssertSuccess().
		AssertJsonHas("data.command_id").
		AssertJsonHas("data.trace_id").
		AssertHeaderExists("X-Request-ID")

	s.Len(s.Publisher.Published, 1)
	s.Equal(events.Topics.AccountCommands, s.Publisher.Last().Topic)
	s.Equal(events.EventTypes.CreateAccount, s.Publisher.Last().Event.Type)
}

func (s *AccountsTestSuite) TestCreateAccountEchoesRequestIDAsTraceID() {
	s.WithHeader("X-Request-ID", "trace-abc").
		Post("/api/v1/accounts", s.validAccount()).
		AssertAccepted().
		AssertJsonPath("data.trace_id", "trace-abc")

	s.Equal("trace-abc", s.Publisher.Last().Event.TraceID)
}

func (s *AccountsTestSuite) TestCreateAccountValidationError() {
	s.Post("/api/v1/accounts", map[string]interface{}{"name": "x"}).
		AssertUnprocessableEntity().
		AssertError().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.user_id", "required")

	s.Empty(s.Publisher.Published)
}

func (s *AccountsTestSuite) TestCreateAccountWhenBrokerIsDown() {
	s.Publisher.Err = apperrors.ServiceUnavailable("PUBLISH_FAILED", "down")

	s.Post("/api/v1/accounts", s.validAccount()).
		AssertStatus(503).
		AssertErrorCode("PUBLISH_FAILED")
}

func (s *AccountsTestSuite) TestUpdateAccountIsAccepted() {
	id := tests.UUID()

	s.Patch("/api/v1/accounts/"+id, map[string]interface{}{"status": "closed"}).
		AssertAccepted().
		AssertSuccess()

	s.Equal(id, s.Publisher.Last().Key)
	s.Equal(events.EventTypes.UpdateAccount, s.Publisher.Last().Event.Type)
}

func (s *AccountsTestSuite) TestDeleteAccountIsAccepted() {
	id := tests.UUID()

	s.Delete("/api/v1/accounts/" + id).
		AssertAccepted().
		AssertSuccess()

	s.Equal(events.EventTypes.DeleteAccount, s.Publisher.Last().Event.Type)
}

func (s *AccountsTestSuite) TestAccountsRejectUnsupportedMethods() {
	s.Get("/api/v1/accounts").AssertMethodNotAllowed()
	s.Put("/api/v1/accounts/"+tests.UUID(), nil).AssertMethodNotAllowed()
}
