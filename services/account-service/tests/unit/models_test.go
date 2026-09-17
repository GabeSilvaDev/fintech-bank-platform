package unit

import (
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/stretchr/testify/assert"
)

func TestParseAccountType(t *testing.T) {
	kind, ok := models.ParseAccountType("savings")
	assert.True(t, ok)
	assert.Equal(t, models.AccountTypeSavings, kind)

	_, ok = models.ParseAccountType("gold")
	assert.False(t, ok)
}

func TestParseAccountStatus(t *testing.T) {
	status, ok := models.ParseAccountStatus("blocked")
	assert.True(t, ok)
	assert.Equal(t, models.AccountStatusBlocked, status)

	_, ok = models.ParseAccountStatus("deleted")
	assert.False(t, ok)
}
