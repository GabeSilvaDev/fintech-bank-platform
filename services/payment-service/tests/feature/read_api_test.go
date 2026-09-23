package feature

import (
	"errors"
	"testing"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/tests"
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
	payment := &models.Payment{ID: uuid.New(), AccountID: uuid.New(), Method: models.MethodPix, Status: models.StatusCompleted, AmountCents: 250, Currency: "BRL", Recipient: "Ana", PixKey: "ana@example.com", IdempotencyKey: "k"}
	s.Repo.Put(payment)

	s.WithHeader("X-Request-ID", "read-1").
		Get("/payments/"+payment.ID.String()).
		AssertOk().AssertSuccess().AssertJsonPath("data.amount", 2.5).AssertHeader("X-Request-ID", "read-1")
	s.Get("/accounts/"+payment.AccountID.String()+"/payments").AssertOk().AssertJsonCount(1, "data")
	s.Get("/payments/" + uuid.NewString()).AssertNotFound().AssertErrorCode("PAYMENT_NOT_FOUND")
	s.Get("/nope").AssertNotFound()
}
