package unit

import (
	"testing"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/stretchr/testify/assert"
)

func TestParseChannel(t *testing.T) {
	for _, raw := range []string{"email", "sms", "push"} {
		channel, ok := models.ParseChannel(raw)
		assert.True(t, ok)
		assert.Equal(t, models.Channel(raw), channel)
	}
	for _, raw := range []string{"", "EMAIL", "fax"} {
		_, ok := models.ParseChannel(raw)
		assert.False(t, ok, raw)
	}
}

func TestMaskRecipient(t *testing.T) {
	assert.Equal(t, "a***@example.com", models.MaskRecipient(models.ChannelEmail, "ana@example.com"))
	assert.Equal(t, "+55*******7766", models.MaskRecipient(models.ChannelSMS, "+5511999887766"))
	userID := "5b1f7c2e-1f0a-4f55-9c1e-0d1d2f3a4b5c"
	assert.Equal(t, userID, models.MaskRecipient(models.ChannelPush, userID))
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"ana@example.com":  "a***@example.com",
		"a@example.com":    "a***@example.com",
		"élise@example.fr": "é***@example.fr",
		"@example.com":     "***@example.com",
		"no-at-sign":       "**********",
		"":                 "",
	}
	for address, want := range cases {
		assert.Equal(t, want, models.MaskEmail(address), address)
	}
}

func TestMaskPhone(t *testing.T) {
	cases := map[string]string{
		"+5511999887766": "+55*******7766",
		"11999887766":    "119****7766",
		"12345678":       "123*5678",
		"1234567":        "*******",
		"":               "",
	}
	for phone, want := range cases {
		assert.Equal(t, want, models.MaskPhone(phone), phone)
	}
}
