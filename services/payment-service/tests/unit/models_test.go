package unit

import (
	"testing"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestParseMethod(t *testing.T) {
	for _, raw := range []string{"pix", "ted", "boleto"} {
		method, ok := models.ParseMethod(raw)
		assert.True(t, ok)
		assert.Equal(t, models.Method(raw), method)
	}
	for _, raw := range []string{"", "PIX", "doc", "card"} {
		_, ok := models.ParseMethod(raw)
		assert.False(t, ok, raw)
	}
}

func TestReferencesAndStepKeys(t *testing.T) {
	id := uuid.New()

	assert.Equal(t, "payment:"+id.String(), models.Reference(id))
	parsed, ok := models.ParseReference(models.Reference(id))
	assert.True(t, ok)
	assert.Equal(t, id, parsed)
	for _, bad := range []string{"", id.String(), "payment:", "payment:nope", "transaction:" + id.String()} {
		_, ok := models.ParseReference(bad)
		assert.False(t, ok, bad)
	}

	key := models.StepKey(id, models.StepRefund)
	assert.Equal(t, "payment:"+id.String()+":refund", key)
	parsed, step, ok := models.ParseStepKey(key)
	assert.True(t, ok)
	assert.Equal(t, id, parsed)
	assert.Equal(t, models.StepRefund, step)
	_, step, ok = models.ParseStepKey(models.StepKey(id, models.StepDebit))
	assert.True(t, ok)
	assert.Equal(t, models.StepDebit, step)
	for _, bad := range []string{"", id.String() + ":debit", "payment:" + id.String(), "payment:x:debit", "payment:" + id.String() + ":credit", "order:" + id.String() + ":debit", "payment:" + id.String() + ":debit:x"} {
		_, _, ok := models.ParseStepKey(bad)
		assert.False(t, ok, bad)
	}
}

func TestParseSettlementStatus(t *testing.T) {
	status, ok := models.ParseSettlementStatus("settled")
	assert.True(t, ok)
	assert.Equal(t, models.SubmissionSettled, status)
	status, ok = models.ParseSettlementStatus("rejected")
	assert.True(t, ok)
	assert.Equal(t, models.SubmissionRejected, status)
	for _, bad := range []string{"", "pending", "SETTLED"} {
		_, ok := models.ParseSettlementStatus(bad)
		assert.False(t, ok, bad)
	}
}

func TestStatusTerminal(t *testing.T) {
	for _, status := range []models.Status{models.StatusCompleted, models.StatusFailed, models.StatusRefunded, models.StatusRefundFailed} {
		assert.True(t, status.Terminal(), status)
	}
	for _, status := range []models.Status{models.StatusPending, models.StatusDebited, models.StatusSubmitted, models.StatusRefunding} {
		assert.False(t, status.Terminal(), status)
	}
}
