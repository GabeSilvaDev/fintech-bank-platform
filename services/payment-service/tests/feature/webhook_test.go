package feature

import (
	"strconv"
	"testing"

	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/stretchr/testify/suite"
)

type WebhookSuite struct {
	tests.TestCase
}

func TestWebhookSuite(t *testing.T) {
	suite.Run(t, new(WebhookSuite))
}

func (s *WebhookSuite) signed(body string) *WebhookSuite {
	ts := strconv.FormatInt(s.Clock.T.Unix(), 10)
	s.WithHeader("X-Timestamp", ts)
	s.WithHeader("X-Signature", services.Sign(tests.WebhookSecret, ts, []byte(body)))
	return s
}

func (s *WebhookSuite) TestSignedWebhookIsQueued() {
	body := `{"external_id":"ted_1","status":"settled"}`
	s.signed(body).WithHeader("X-Request-ID", "hook-9").PostRaw("/webhooks/gateway", []byte(body)).AssertStatus(202).AssertJsonHas("data.command_id")

	s.Len(s.Publisher.Published, 1)
	s.Equal(events.Topics.PaymentCommands, s.Publisher.Published[0].Topic)
	s.Equal("hook-9", s.Publisher.Published[0].Event.TraceID)
}

func (s *WebhookSuite) TestUnsignedWebhookIsRejected() {
	s.PostRaw("/webhooks/gateway", []byte(`{"external_id":"ted_1","status":"settled"}`)).AssertStatus(401).AssertErrorCode("INVALID_SIGNATURE")
	s.Empty(s.Publisher.Published)
}

func (s *WebhookSuite) TestWebhookRouteOnlyAcceptsPost() {
	s.Get("/webhooks/gateway").AssertMethodNotAllowed()
}
