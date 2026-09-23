package unit

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/app/services"
	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/notification-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var sentAt = time.Date(2026, 9, 23, 18, 0, 0, 0, time.UTC)

type deliveryHarness struct {
	email, sms, push *tests.FakeSender
	history          *tests.FakeHistory
	logs             *bytes.Buffer
	delivery         *services.Delivery
}

func newDeliveryHarness() *deliveryHarness {
	h := &deliveryHarness{email: &tests.FakeSender{}, sms: &tests.FakeSender{}, push: &tests.FakeSender{}, history: &tests.FakeHistory{}, logs: &bytes.Buffer{}}
	senders := map[models.Channel]contracts.Sender{models.ChannelEmail: h.email, models.ChannelSMS: h.sms, models.ChannelPush: h.push}
	h.delivery = services.NewDelivery(senders, h.history, tests.FakeClock{T: sentAt}, logger.New(logger.Config{Output: h.logs}))
	return h
}

func notification(eventType string, payload interface{}, userID uuid.UUID) *events.Event {
	return events.NewNotificationEvent(eventType, payload).WithMetadata("user_id", userID.String()).WithMetadata("source_event_id", "src-1")
}

func TestDeliverEveryChannel(t *testing.T) {
	h := newDeliveryHarness()
	user := uuid.New()

	email := notification(events.EventTypes.SendEmail, events.SendEmailPayload{To: "ana@example.com", Subject: "Oi", Template: "welcome", Data: map[string]string{"body": "Olá"}, Priority: "normal"}, user)
	res, err := h.delivery.Dispatch(context.Background(), email)
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Equal(t, []models.Message{{Channel: models.ChannelEmail, UserID: user, To: "ana@example.com", Subject: "Oi", Body: "Olá", Priority: "normal"}}, h.email.Sent)
	assert.Equal(t, models.Record{ID: email.ID, UserID: user, Channel: models.ChannelEmail, Recipient: "ana@example.com", Subject: "Oi", Body: "Olá", SourceEventID: "src-1", SentAt: sentAt}, h.history.Records[0])

	_, err = h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendSMS, events.SendSMSPayload{To: "+5511999887766", Message: "Pagamento", Priority: "high"}, user))
	assert.NoError(t, err)
	assert.Equal(t, models.Message{Channel: models.ChannelSMS, UserID: user, To: "+5511999887766", Body: "Pagamento", Priority: "high"}, h.sms.Sent[0])

	_, err = h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendPush, events.SendPushPayload{UserID: user.String(), Title: "T", Body: "B", Priority: "normal"}, user))
	assert.NoError(t, err)
	assert.Equal(t, models.Message{Channel: models.ChannelPush, UserID: user, To: user.String(), Subject: "T", Body: "B", Priority: "normal"}, h.push.Sent[0])
	assert.Len(t, h.history.Records, 3)
}

func TestDeliveryRejectsBadCommands(t *testing.T) {
	h := newDeliveryHarness()
	user := uuid.New()

	_, err := h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendEmail, events.SendEmailPayload{To: ""}, user))
	assert.Equal(t, "invalid_recipient", domain.InvalidCode(err))
	_, err = h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendSMS, events.SendSMSPayload{To: ""}, user))
	assert.Equal(t, "invalid_recipient", domain.InvalidCode(err))
	_, err = h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendPush, events.SendPushPayload{UserID: ""}, user))
	assert.Equal(t, "invalid_recipient", domain.InvalidCode(err))

	_, err = h.delivery.Dispatch(context.Background(), events.NewNotificationEvent(events.EventTypes.SendPush, events.SendPushPayload{UserID: "u"}))
	assert.Equal(t, "invalid_user_id", domain.InvalidCode(err))

	for _, eventType := range []string{events.EventTypes.SendEmail, events.EventTypes.SendSMS, events.EventTypes.SendPush} {
		_, err = h.delivery.Dispatch(context.Background(), notification(eventType, "not an object", user))
		assert.ErrorIs(t, err, processor.ErrBadPayload, eventType)
	}

	_, err = h.delivery.Dispatch(context.Background(), notification("notification.fax", nil, user))
	assert.ErrorIs(t, err, processor.ErrUnknownCommand)

	partial := services.NewDelivery(map[models.Channel]contracts.Sender{}, h.history, tests.FakeClock{T: sentAt}, logger.New(logger.Config{Output: h.logs}))
	_, err = partial.Dispatch(context.Background(), notification(events.EventTypes.SendPush, events.SendPushPayload{UserID: user.String()}, user))
	assert.Equal(t, "unsupported_channel", domain.InvalidCode(err))
	assert.Empty(t, h.history.Records)
}

func TestDeliveryFailuresAndHistoryErrors(t *testing.T) {
	h := newDeliveryHarness()
	user := uuid.New()

	h.push.Errs = []error{errors.New("fcm down")}
	_, err := h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendPush, events.SendPushPayload{UserID: user.String(), Title: "T"}, user))
	assert.EqualError(t, err, "fcm down")
	assert.Empty(t, h.history.Records)

	h.history.Err = errors.New("redis down")
	_, err = h.delivery.Dispatch(context.Background(), notification(events.EventTypes.SendPush, events.SendPushPayload{UserID: user.String(), Title: "T"}, user))
	assert.NoError(t, err)
	assert.Len(t, h.push.Sent, 1)
	assert.Contains(t, h.logs.String(), "notification history not recorded")
}
