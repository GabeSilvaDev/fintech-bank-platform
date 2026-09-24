package models

import (
	"time"

	"github.com/google/uuid"
)

type Identity struct {
	Email        string
	UserID       uuid.UUID
	PasswordHash string
	CreatedAt    time.Time
}

type LoginFailure struct {
	Email        string
	Failures     int
	FirstFailure time.Time
}
