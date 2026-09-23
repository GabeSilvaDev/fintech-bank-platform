package contracts

import (
	"context"

	"github.com/fintech-bank-platform/pkg/events"
)

type Publisher interface {
	Publish(ctx context.Context, topic, key string, event *events.Event) error
}
