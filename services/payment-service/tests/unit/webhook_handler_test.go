package unit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/stretchr/testify/assert"
)

func webhookCall(publisher *tests.FakePublisher, body string, sign func(ts string, body []byte) (string, string)) (*httptest.ResponseRecorder, map[string]interface{}) {
	handler := middleware.RequestID(http.HandlerFunc(handlers.NewWebhookHandler(publisher, "s3cret", 5*time.Minute, tests.FakeClock{T: now}, logger.New(logger.Config{Output: io.Discard})).Gateway))
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gateway", bytes.NewBufferString(body))
	ts, signature := sign(strconv.FormatInt(now.Unix(), 10), []byte(body))
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", signature)
	req.Header.Set(middleware.RequestIDHeader, "hook-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, tests.FromJson(rec.Body.String())
}

func signed(ts string, body []byte) (string, string) {
	return ts, services.Sign("s3cret", ts, body)
}

func TestWebhookPublishesASettleCommand(t *testing.T) {
	publisher := &tests.FakePublisher{}

	rec, body := webhookCall(publisher, `{"external_id":"ted_1","status":"rejected","reason":"invalid_destination"}`, signed)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	published := publisher.Published[0]
	assert.Equal(t, events.Topics.PaymentCommands, published.Topic)
	assert.Equal(t, "ted_1", published.Key)
	assert.Equal(t, events.EventTypes.SettlePayment, published.Event.Type)
	assert.Equal(t, "payment-service", published.Event.Source)
	assert.Equal(t, "hook-1", published.Event.TraceID)
	assert.Equal(t, events.SettlePaymentPayload{ExternalID: "ted_1", Status: "rejected", Reason: "invalid_destination"}, published.Event.Payload)
	assert.Equal(t, published.Event.ID, body["data"].(map[string]interface{})["command_id"])
}

func TestWebhookPublishesTheTrimmedExternalIDAndCapsTheReason(t *testing.T) {
	publisher := &tests.FakePublisher{}

	rec, _ := webhookCall(publisher, `{"external_id":" ted_1 ","status":"rejected","reason":"`+strings.Repeat("r", 300)+`"}`, signed)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	published := publisher.Published[0]
	assert.Equal(t, "ted_1", published.Key)
	payload := published.Event.Payload.(events.SettlePaymentPayload)
	assert.Equal(t, "ted_1", payload.ExternalID)
	assert.Equal(t, strings.Repeat("r", 255), payload.Reason)

	publisher = &tests.FakePublisher{}
	multibyte := strings.Repeat("r", 254) + "é" + strings.Repeat("r", 10)
	webhookCall(publisher, `{"external_id":"ted_2","status":"rejected","reason":"`+multibyte+`"}`, signed)
	reason := publisher.Published[0].Event.Payload.(events.SettlePaymentPayload).Reason
	assert.Equal(t, strings.Repeat("r", 254), reason)
	assert.True(t, utf8.ValidString(reason))
}

func TestWebhookRejectsBadRequests(t *testing.T) {
	cases := []struct {
		body   string
		sign   func(string, []byte) (string, string)
		status int
		code   string
	}{
		{`{"external_id":"ted_1","status":"settled"}`, func(ts string, _ []byte) (string, string) { return ts, "deadbeef" }, http.StatusUnauthorized, "INVALID_SIGNATURE"},
		{`{"external_id":"ted_1","status":"settled"}`, func(_ string, b []byte) (string, string) { return "1", services.Sign("s3cret", "1", b) }, http.StatusUnauthorized, "INVALID_SIGNATURE"},
		{`not json`, signed, http.StatusBadRequest, "INVALID_JSON"},
		{`{"external_id":" ","status":"pending"}`, signed, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{`{"external_id":"x","status":"settled","reason":"` + strings.Repeat("r", 70<<10) + `"}`, signed, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
	}
	for i, c := range cases {
		publisher := &tests.FakePublisher{}
		rec, body := webhookCall(publisher, c.body, c.sign)
		assert.Equal(t, c.status, rec.Code, i)
		assert.Equal(t, c.code, body["error"].(map[string]interface{})["code"], i)
		assert.Empty(t, publisher.Published, i)
	}

	_, body := webhookCall(&tests.FakePublisher{}, `{"external_id":"","status":"nope"}`, signed)
	details := body["error"].(map[string]interface{})["details"].(map[string]interface{})
	assert.Equal(t, "required", details["external_id"])
	assert.Equal(t, "oneof", details["status"])
}

func TestWebhookAnswers503WhenPublishFails(t *testing.T) {
	rec, body := webhookCall(&tests.FakePublisher{Err: errors.New("broker down")}, `{"external_id":"ted_1","status":"settled"}`, signed)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "PUBLISH_FAILED", body["error"].(map[string]interface{})["code"])
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
func (failingBody) Close() error             { return nil }

func TestWebhookReportsUnreadableBodies(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gateway", nil)
	req.Body = failingBody{}
	rec := httptest.NewRecorder()
	handlers.NewWebhookHandler(&tests.FakePublisher{}, "s3cret", time.Minute, tests.FakeClock{T: now}, logger.New(logger.Config{Output: io.Discard})).Gateway(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_BODY", tests.FromJson(rec.Body.String())["error"].(map[string]interface{})["code"])
}

func TestWebhookLogsRejectedSignatures(t *testing.T) {
	logs := &bytes.Buffer{}
	handler := handlers.NewWebhookHandler(&tests.FakePublisher{}, "s3cret", 5*time.Minute, tests.FakeClock{T: now}, logger.New(logger.Config{Output: logs}))
	body := `{"external_id":"ted_secret_body","status":"settled"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gateway", bytes.NewBufferString(body))
	req.RemoteAddr = "10.0.0.7:5555"
	req.Header.Set("X-Timestamp", "1700000000")
	req.Header.Set("X-Signature", "deadbeefcafe")
	rec := httptest.NewRecorder()

	handler.Gateway(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var entry map[string]interface{}
	assert.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	assert.Equal(t, "warn", entry["level"])
	assert.Equal(t, "invalid webhook signature", entry["message"])
	assert.Equal(t, "10.0.0.7:5555", entry["remote_addr"])
	assert.Equal(t, "1700000000", entry["timestamp"])
	assert.NotContains(t, logs.String(), "deadbeefcafe")
	assert.NotContains(t, logs.String(), "ted_secret_body")

	logs.Reset()
	valid := []byte(`{"external_id":"ted_1","status":"settled"}`)
	ts := strconv.FormatInt(now.Unix(), 10)
	req = httptest.NewRequest(http.MethodPost, "/webhooks/gateway", bytes.NewReader(valid))
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", services.Sign("s3cret", ts, valid))
	rec = httptest.NewRecorder()
	handler.Gateway(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, logs.String())
}
