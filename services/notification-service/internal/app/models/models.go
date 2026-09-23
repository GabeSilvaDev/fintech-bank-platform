package models

import (
	"time"

	"github.com/google/uuid"
)

type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

const (
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

type Contact struct {
	AccountID uuid.UUID
	UserID    uuid.UUID
	Name      string
	Email     string
	Phone     string
}

type Message struct {
	Channel  Channel
	UserID   uuid.UUID
	To       string
	Subject  string
	Body     string
	Priority string
}

type Record struct {
	ID            string
	UserID        uuid.UUID
	Channel       Channel
	Recipient     string
	Subject       string
	Body          string
	SourceEventID string
	SentAt        time.Time
}

func ParseChannel(s string) (Channel, bool) {
	switch Channel(s) {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return Channel(s), true
	}
	return "", false
}
