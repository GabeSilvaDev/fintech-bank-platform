package feature

import (
	"errors"
	"testing"

	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type ReadAPISuite struct {
	tests.TestCase
}

func TestReadAPISuite(t *testing.T) {
	suite.Run(t, new(ReadAPISuite))
}

func (s *ReadAPISuite) TestHealth() {
	s.Get("/health").AssertOk().AssertJsonPath("data.cassandra", "up")
	s.PingErr = errors.New("down")
	s.Get("/health").AssertStatus(503).AssertJsonPath("data.status", "degraded")
}

func (s *ReadAPISuite) TestReadsThroughRouter() {
	tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusCompleted, AccountID: uuid.New(), AmountCents: 250, Currency: "BRL", IdempotencyKey: "k"}
	s.Repo.Put(tx)

	s.WithHeader("X-Request-ID", "read-1").
		Get("/transactions/"+tx.ID.String()).
		AssertOk().AssertSuccess().AssertJsonPath("data.amount", 2.5).AssertHeader("X-Request-ID", "read-1")
	s.Get("/accounts/"+tx.AccountID.String()+"/transactions").AssertOk().AssertJsonCount(1, "data")
	s.Get("/transactions/" + uuid.NewString()).AssertNotFound().AssertErrorCode("TRANSACTION_NOT_FOUND")
	s.Get("/nope").AssertNotFound()
}
