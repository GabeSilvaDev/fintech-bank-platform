package senders

import (
	"context"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/logger"
)

type Sandbox struct {
	channel models.Channel
	log     *logger.Logger
}

func NewSandbox(channel models.Channel, log *logger.Logger) *Sandbox {
	return &Sandbox{channel: channel, log: log}
}

func (s *Sandbox) Send(_ context.Context, message models.Message) error {
	s.log.Info().
		Str("channel", string(s.channel)).
		Str("to", message.To).
		Str("priority", message.Priority).
		Str("subject", message.Subject).
		Str("body", message.Body).
		Msg("notification sent")
	return nil
}
