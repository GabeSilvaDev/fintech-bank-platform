package services

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
)

type Router struct {
	directory contracts.Directory
	renderer  *Renderer
	clock     contracts.Clock
	maxAge    time.Duration
	log       *logger.Logger
}

func NewRouter(directory contracts.Directory, renderer *Renderer, clock contracts.Clock, maxAge time.Duration, log *logger.Logger) *Router {
	return &Router{directory: directory, renderer: renderer, clock: clock, maxAge: maxAge, log: log}
}

type notice struct {
	accountID string
	kind      string
	priority  string
	data      map[string]string
	channels  []models.Channel
}

func (r *Router) Dispatch(ctx context.Context, event *events.Event) (processor.Result, error) {
	if age := r.clock.Now().Sub(event.Timestamp); r.maxAge > 0 && !event.Timestamp.IsZero() && age > r.maxAge {
		r.log.Info().Str("event_id", event.ID).Str("event_type", event.Type).Dur("age", age).Msg("stale event skipped")
		return processor.Result{}, nil
	}
	notices, err := notices(event)
	if err != nil {
		return processor.Result{}, err
	}
	var messages []processor.Message
	for _, n := range notices {
		produced, err := r.notify(ctx, event, n)
		if err != nil {
			return processor.Result{}, err
		}
		messages = append(messages, produced...)
	}
	return processor.Result{Messages: messages}, nil
}

func (r *Router) notify(ctx context.Context, source *events.Event, n notice) ([]processor.Message, error) {
	id, err := uuid.Parse(n.accountID)
	if err != nil {
		return nil, nil
	}
	contact, err := r.directory.Lookup(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := map[string]string{"name": contact.Name}
	for key, value := range n.data {
		data[key] = value
	}
	rendered, err := r.renderer.Render(n.kind, data)
	if err != nil {
		return nil, domain.Invalid("template_error", err.Error())
	}
	var messages []processor.Message
	for _, channel := range n.channels {
		command := buildCommand(channel, contact, rendered, n.kind, n.priority)
		if command == nil {
			continue
		}
		command.WithTraceID(source.TraceID).WithMetadata("user_id", contact.UserID.String()).WithMetadata("source_event_id", source.ID)
		messages = append(messages, processor.Message{Topic: events.Topics.NotificationEvents, Key: contact.UserID.String(), Event: command})
	}
	return messages, nil
}

func buildCommand(channel models.Channel, contact models.Contact, rendered Rendered, kind, priority string) *events.Event {
	switch channel {
	case models.ChannelEmail:
		if contact.Email == "" {
			return nil
		}
		return events.NewNotificationEvent(events.EventTypes.SendEmail, events.SendEmailPayload{To: contact.Email, Subject: rendered.Subject, Template: kind, Data: map[string]string{"body": rendered.Body}, Priority: priority})
	case models.ChannelSMS:
		if contact.Phone == "" {
			return nil
		}
		return events.NewNotificationEvent(events.EventTypes.SendSMS, events.SendSMSPayload{To: contact.Phone, Message: rendered.Body, Priority: priority})
	}
	return events.NewNotificationEvent(events.EventTypes.SendPush, events.SendPushPayload{UserID: contact.UserID.String(), Title: rendered.Subject, Body: rendered.Body, Data: map[string]string{"kind": kind}, Priority: priority})
}

func notices(event *events.Event) ([]notice, error) {
	push := []models.Channel{models.ChannelPush}
	pushEmail := []models.Channel{models.ChannelPush, models.ChannelEmail}

	switch event.Type {
	case events.EventTypes.AccountCreated:
		var p events.AccountCreatedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		return []notice{{
			accountID: p.AccountID,
			kind:      "welcome",
			priority:  models.PriorityNormal,
			data:      map[string]string{"account_type": AccountTypeLabel(p.AccountType), "agency": p.Agency, "account_number": p.AccountNumber},
			channels:  []models.Channel{models.ChannelEmail},
		}}, nil
	case events.EventTypes.TransactionCompleted:
		var p events.TransactionCompletedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		title, _ := OperationLabel(p.Type)
		return []notice{{
			accountID: p.AccountID,
			kind:      "transaction_completed",
			priority:  models.PriorityNormal,
			data:      map[string]string{"operation": title, "amount": FormatBRL(p.Amount), "balance": FormatBRL(p.BalanceAfter)},
			channels:  push,
		}}, nil
	case events.EventTypes.TransactionFailed:
		var p events.TransactionFailedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		title, lower := OperationLabel(p.Type)
		return []notice{{
			accountID: p.AccountID,
			kind:      "transaction_failed",
			priority:  models.PriorityHigh,
			data:      map[string]string{"operation": title, "operation_lower": lower, "amount": FormatBRL(p.Amount), "reason": ReasonText(p.Reason)},
			channels:  pushEmail,
		}}, nil
	case events.EventTypes.TransferCompleted:
		var p events.TransferCompletedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		return []notice{
			{
				accountID: p.FromAccountID,
				kind:      "transfer_sent",
				priority:  models.PriorityNormal,
				data:      map[string]string{"amount": FormatBRL(p.Amount), "balance": FormatBRL(p.FromBalanceAfter)},
				channels:  push,
			},
			{
				accountID: p.ToAccountID,
				kind:      "transfer_received",
				priority:  models.PriorityNormal,
				data:      map[string]string{"amount": FormatBRL(p.Amount), "balance": FormatBRL(p.ToBalanceAfter)},
				channels:  push,
			},
		}, nil
	case events.EventTypes.TransferFailed:
		var p events.TransferFailedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		return []notice{{
			accountID: p.FromAccountID,
			kind:      "transfer_failed",
			priority:  models.PriorityHigh,
			data:      map[string]string{"amount": FormatBRL(p.Amount), "reason": ReasonText(p.Reason), "outcome": TransferOutcome(p.Status)},
			channels:  pushEmail,
		}}, nil
	case events.EventTypes.PaymentCompleted:
		var p events.PaymentCompletedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		return []notice{{
			accountID: p.AccountID,
			kind:      "payment_completed",
			priority:  models.PriorityNormal,
			data:      map[string]string{"method": MethodLabel(p.PaymentMethod), "amount": FormatBRL(p.Amount)},
			channels:  pushEmail,
		}}, nil
	case events.EventTypes.PaymentFailed:
		var p events.PaymentFailedPayload
		if err := processor.DecodePayload(event, &p); err != nil {
			return nil, err
		}
		channels := pushEmail
		if p.Status == "refund_failed" {
			channels = []models.Channel{models.ChannelPush, models.ChannelEmail, models.ChannelSMS}
		}
		return []notice{{
			accountID: p.AccountID,
			kind:      "payment_failed",
			priority:  models.PriorityHigh,
			data:      map[string]string{"method": MethodLabel(p.PaymentMethod), "amount": FormatBRL(p.Amount), "reason": ReasonText(p.Reason), "outcome": PaymentOutcome(p.Status)},
			channels:  channels,
		}}, nil
	}
	return nil, nil
}
