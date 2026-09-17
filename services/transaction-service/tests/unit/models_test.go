package unit

import (
	"testing"

	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestParseCreateType(t *testing.T) {
	kind, ok := models.ParseCreateType("withdrawal")
	assert.True(t, ok)
	assert.Equal(t, models.TypeWithdrawal, kind)

	_, ok = models.ParseCreateType("transfer")
	assert.False(t, ok)
	_, ok = models.ParseCreateType("refund")
	assert.False(t, ok)
}

func TestStepKeys(t *testing.T) {
	id := uuid.New()

	key := models.StepKey(id, models.StepReversal)
	assert.Equal(t, id.String()+":reversal", key)

	parsed, step, ok := models.ParseStepKey(key)
	assert.True(t, ok)
	assert.Equal(t, id, parsed)
	assert.Equal(t, models.StepReversal, step)

	for _, bad := range []string{"", "nope", id.String(), id.String() + ":refund", "x:debit", id.String() + ":debit:extra"} {
		_, _, ok := models.ParseStepKey(bad)
		assert.False(t, ok, bad)
	}
}
