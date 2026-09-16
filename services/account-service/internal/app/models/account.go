package models

import (
	"time"

	"github.com/google/uuid"
)

type AccountType string

const (
	AccountTypeChecking AccountType = "checking"
	AccountTypeSavings  AccountType = "savings"
)

type AccountStatus string

const (
	AccountStatusActive  AccountStatus = "active"
	AccountStatusBlocked AccountStatus = "blocked"
	AccountStatusClosed  AccountStatus = "closed"
)

const (
	Agency   = "0001"
	Currency = "BRL"
)

type Account struct {
	AccountID    uuid.UUID
	UserID       uuid.UUID
	Agency       string
	Number       string
	Type         AccountType
	Status       AccountStatus
	Currency     string
	BalanceCents int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ClosedAt     *time.Time
}

func ParseAccountType(s string) (AccountType, bool) {
	switch AccountType(s) {
	case AccountTypeChecking, AccountTypeSavings:
		return AccountType(s), true
	}
	return "", false
}

func ParseAccountStatus(s string) (AccountStatus, bool) {
	switch AccountStatus(s) {
	case AccountStatusActive, AccountStatusBlocked, AccountStatusClosed:
		return AccountStatus(s), true
	}
	return "", false
}
