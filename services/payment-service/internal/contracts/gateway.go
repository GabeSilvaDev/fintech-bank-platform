package contracts

import (
	"context"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
)

type Gateway interface {
	Submit(ctx context.Context, payment *models.Payment) (models.Submission, error)
}
