package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Method string

const (
	MethodPix    Method = "pix"
	MethodTED    Method = "ted"
	MethodBoleto Method = "boleto"
)

type Status string

const (
	StatusPending      Status = "pending"
	StatusDebited      Status = "debited"
	StatusSubmitted    Status = "submitted"
	StatusCompleted    Status = "completed"
	StatusFailed       Status = "failed"
	StatusRefunding    Status = "refunding"
	StatusRefunded     Status = "refunded"
	StatusRefundFailed Status = "refund_failed"
)

func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusRefunded, StatusRefundFailed:
		return true
	}
	return false
}

const Currency = "BRL"

var ErrDuplicateKey = errors.New("duplicate idempotency key")

type TEDDetails struct {
	BankCode string
	Branch   string
	Account  string
	Document string
}

type Payment struct {
	ID                uuid.UUID
	AccountID         uuid.UUID
	Method            Method
	Status            Status
	AmountCents       int64
	Currency          string
	Recipient         string
	PixKey            string
	BoletoCode        string
	TED               *TEDDetails
	Description       string
	IdempotencyKey    string
	ExternalID        string
	FailureReason     string
	BalanceAfterCents *int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
}

type Patch struct {
	ExternalID        *string
	FailureReason     *string
	BalanceAfterCents *int64
	CompletedAt       *time.Time
	UpdatedAt         time.Time
}

func ParseMethod(s string) (Method, bool) {
	switch Method(s) {
	case MethodPix, MethodTED, MethodBoleto:
		return Method(s), true
	}
	return "", false
}
