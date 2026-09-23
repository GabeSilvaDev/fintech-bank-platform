package services

import (
	"context"
	"fmt"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	SentTotalName = "notifications_sent_total"
	SentTotalHelp = "Total number of notification send attempts by channel and outcome"
)

type Delivery struct {
	senders map[models.Channel]contracts.Sender
	history contracts.History
	clock   contracts.Clock
	log     *logger.Logger
	sent    *prometheus.CounterVec
}

func NewDelivery(senders map[models.Channel]contracts.Sender, history contracts.History, clock contracts.Clock, log *logger.Logger) *Delivery {
	return (&Delivery{senders: senders, history: history, clock: clock, log: log}).WithMetrics(nil)
}

func (d *Delivery) WithMetrics(m *metrics.Metrics) *Delivery {
	d.sent = m.CounterVec(SentTotalName, SentTotalHelp, "channel", "outcome")
	return d
}

func (d *Delivery) Dispatch(ctx context.Context, event *events.Event) (processor.Result, error) {
	message, err := toMessage(event)
	if err != nil {
		return processor.Result{}, err
	}
	if message.To == "" {
		return processor.Result{}, domain.Invalid("invalid_recipient", "notification has no recipient")
	}
	userID, err := uuid.Parse(event.Metadata["user_id"])
	if err != nil {
		return processor.Result{}, domain.Invalid("invalid_user_id", "metadata user_id must be a uuid")
	}
	message.UserID = userID
	sender, ok := d.senders[message.Channel]
	if !ok {
		return processor.Result{}, domain.Invalid("unsupported_channel", "no sender for channel "+string(message.Channel))
	}
	if err := sender.Send(ctx, message); err != nil {
		d.sent.WithLabelValues(string(message.Channel), "error").Inc()
		return processor.Result{}, err
	}
	d.sent.WithLabelValues(string(message.Channel), "ok").Inc()

	record := models.Record{
		ID:            event.ID,
		UserID:        userID,
		Channel:       message.Channel,
		Recipient:     message.To,
		Subject:       message.Subject,
		Body:          message.Body,
		SourceEventID: event.Metadata["source_event_id"],
		SentAt:        d.clock.Now(),
	}
	if err := d.history.Append(ctx, record); err != nil {
		d.log.Warn().Err(err).Str("event_id", event.ID).Msg("notification history not recorded")
	}
	return processor.Result{}, nil
}

func toMessage(event *events.Event) (models.Message, error) {
	switch event.Type {
	case events.EventTypes.SendEmail:
		var p events.SendEmailPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return models.Message{}, err
		}
		return models.Message{Channel: models.ChannelEmail, To: p.To, Subject: p.Subject, Body: p.Data["body"], Priority: p.Priority}, nil
	case events.EventTypes.SendSMS:
		var p events.SendSMSPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return models.Message{}, err
		}
		return models.Message{Channel: models.ChannelSMS, To: p.To, Body: p.Message, Priority: p.Priority}, nil
	case events.EventTypes.SendPush:
		var p events.SendPushPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return models.Message{}, err
		}
		return models.Message{Channel: models.ChannelPush, To: p.UserID, Subject: p.Title, Body: p.Body, Priority: p.Priority}, nil
	}
	return models.Message{}, fmt.Errorf("%w: %s", processor.ErrUnknownCommand, event.Type)
}
