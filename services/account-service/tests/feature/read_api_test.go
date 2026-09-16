package feature

import (
	"errors"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type ReadAPISuite struct {
	tests.TestCase
}

func TestReadAPISuite(t *testing.T) {
	suite.Run(t, new(ReadAPISuite))
}

func (s *ReadAPISuite) TestHealthReportsCassandra() {
	s.Get("/health").AssertOk().AssertJsonPath("data.status", "healthy").AssertJsonPath("data.cassandra", "up")

	s.PingErr = errors.New("down")
	s.Get("/health").AssertStatus(503).AssertJsonPath("data.status", "degraded").AssertJsonPath("data.cassandra", "down")
}

func (s *ReadAPISuite) TestGetAccountThroughRouter() {
	account := &models.Account{AccountID: uuid.New(), UserID: uuid.New(), Agency: "0001", Number: "00000001", Type: models.AccountTypeSavings, Status: models.AccountStatusActive, Currency: "BRL", BalanceCents: 250}
	s.Accounts.Put(account)

	s.WithHeader("X-Request-ID", "read-1").
		Get("/accounts/"+account.AccountID.String()).
		AssertOk().
		AssertSuccess().
		AssertJsonPath("data.balance", 2.5).
		AssertHeader("X-Request-ID", "read-1")

	s.Get("/accounts/" + uuid.NewString()).AssertNotFound().AssertErrorCode("ACCOUNT_NOT_FOUND")
	s.Get("/users/"+account.UserID.String()+"/accounts").AssertOk().AssertJsonCount(1, "data")
	s.Get("/nope").AssertNotFound()
}
