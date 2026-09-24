package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type TransactionType string

const (
	TypeDeposit    TransactionType = "deposit"
	TypeWithdrawal TransactionType = "withdrawal"
	TypeTransfer   TransactionType = "transfer"
)

type TransactionStatus string

const (
	StatusPending        TransactionStatus = "pending"
	StatusDebited        TransactionStatus = "debited"
	StatusCompleted      TransactionStatus = "completed"
	StatusFailed         TransactionStatus = "failed"
	StatusReversing      TransactionStatus = "reversing"
	StatusReversed       TransactionStatus = "reversed"
	StatusReversalFailed TransactionStatus = "reversal_failed"
)

func (s TransactionStatus) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusReversed, StatusReversalFailed:
		return true
	}
	return false
}

const Currency = "BRL"

var ErrDuplicateKey = errors.New("duplicate idempotency key")

type Transaction struct {
	ID               uuid.UUID
	Type             TransactionType
	Status           TransactionStatus
	AccountID        uuid.UUID
	CounterpartyID   *uuid.UUID
	AmountCents      int64
	Currency         string
	Description      string
	IdempotencyKey   string
	FailureReason    string
	FromBalanceCents *int64
	ToBalanceCents   *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

type Page struct {
	Items   []*Transaction
	Scanned int
	Last    *time.Time
}

type Patch struct {
	FailureReason    *string
	FromBalanceCents *int64
	ToBalanceCents   *int64
	CompletedAt      *time.Time
	UpdatedAt        time.Time
}

func ParseCreateType(s string) (TransactionType, bool) {
	switch TransactionType(s) {
	case TypeDeposit, TypeWithdrawal:
		return TransactionType(s), true
	}
	return "", false
}
