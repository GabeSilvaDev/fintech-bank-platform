package models

import (
	"strings"
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

func MaskRecipient(channel Channel, recipient string) string {
	switch channel {
	case ChannelEmail:
		return MaskEmail(recipient)
	case ChannelSMS:
		return MaskPhone(recipient)
	}
	return recipient
}

func MaskEmail(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return strings.Repeat("*", len([]rune(address)))
	}
	local := []rune(address[:at])
	prefix := ""
	if len(local) > 0 {
		prefix = string(local[0])
	}
	return prefix + "***" + address[at:]
}

func MaskPhone(phone string) string {
	runes := []rune(phone)
	if len(runes) <= 7 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:3]) + strings.Repeat("*", len(runes)-7) + string(runes[len(runes)-4:])
}
