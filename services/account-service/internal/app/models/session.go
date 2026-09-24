package models

import (
	"time"

	"github.com/google/uuid"
)

type RefreshTokenStatus string

const (
	RefreshTokenActive  RefreshTokenStatus = "active"
	RefreshTokenRotated RefreshTokenStatus = "rotated"
	RefreshTokenRevoked RefreshTokenStatus = "revoked"
)

type RefreshToken struct {
	TokenHash string
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	Status    RefreshTokenStatus
	ExpiresAt time.Time
	CreatedAt time.Time
}
