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
