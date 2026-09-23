package unit

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/services"
	"github.com/stretchr/testify/assert"
)

func TestSystemClockNowIsUTC(t *testing.T) {
	now := services.SystemClock{}.Now()

	assert.WithinDuration(t, time.Now(), now, time.Second)
	assert.Equal(t, time.UTC, now.Location())
}
