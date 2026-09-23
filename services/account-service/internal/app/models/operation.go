package models

import (
	"time"

	"github.com/google/uuid"
)

type OperationStatus string

const (
	OperationPending OperationStatus = "pending"
	OperationDone    OperationStatus = "done"
)

type BalanceOperation struct {
	AccountID uuid.UUID
	Key       string
	Kind      string
	Status    OperationStatus
	Result    string
	CreatedAt time.Time
	UpdatedAt time.Time
}
