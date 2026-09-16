package models

import (
	"time"

	"github.com/google/uuid"
)

const KYCStatusPending = "pending"

type Customer struct {
	UserID    uuid.UUID
	Name      string
	Email     string
	Document  string
	Phone     string
	KYCStatus string
	CreatedAt time.Time
	UpdatedAt time.Time
}
