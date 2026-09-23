package feature

import (
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type HistoryAPISuite struct {
	tests.TestCase
}

func TestHistoryAPISuite(t *testing.T) {
	suite.Run(t, new(HistoryAPISuite))
}

func (s *HistoryAPISuite) TestHealth() {
	s.Get("/health").AssertOk().AssertJsonPath("data.redis", "up")
	s.PingErr = errors.New("down")
	s.Get("/health").AssertStatus(503).AssertJsonPath("data.status", "degraded").AssertJsonPath("data.redis", "down")
}

func (s *HistoryAPISuite) TestListThroughRouter() {
	userID := uuid.New()
	record := models.Record{ID: "n1", UserID: userID, Channel: models.ChannelEmail, Recipient: "ana@example.com", Subject: "Bem-vindo(a)", Body: "Ola Ana", SentAt: time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)}
	s.History.Records = []models.Record{record}

	resp := s.WithHeader("X-Request-ID", "history-1").
		Get("/users/"+userID.String()+"/notifications").
		AssertOk().AssertSuccess().
		AssertJsonCount(1, "data").
		AssertHeader("X-Request-ID", "history-1")

	data := resp.Json()["data"].([]interface{})
	item := data[0].(map[string]interface{})
	s.Equal("n1", item["id"])

	s.Get("/users/" + userID.String() + "/notifications?limit=x").
		AssertUnprocessableEntity().AssertErrorCode("VALIDATION_ERROR")

	s.Get("/users/not-a-uuid/notifications").
		AssertUnprocessableEntity().AssertErrorCode("VALIDATION_ERROR")

	s.History.Err = errors.New("cassandra down")
	s.Get("/users/" + userID.String() + "/notifications").AssertServerError()
}

func (s *HistoryAPISuite) TestUnknownRoute() {
	s.Get("/nope").AssertNotFound()
}
