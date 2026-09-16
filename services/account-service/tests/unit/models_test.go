package unit

import (
	"errors"
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

func TestToCents(t *testing.T) {
	cents, err := models.ToCents(150.5)
	assert.NoError(t, err)
	assert.Equal(t, int64(15050), cents)

	cents, err = models.ToCents(0.1)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), cents)

	_, err = models.ToCents(0)
	assert.True(t, models.IsInvalid(err))
	assert.Equal(t, "invalid_amount", models.InvalidCode(err))

	_, err = models.ToCents(-5)
	assert.True(t, models.IsInvalid(err))

	_, err = models.ToCents(1.005)
	assert.True(t, models.IsInvalid(err))
}

func TestFromCents(t *testing.T) {
	assert.Equal(t, 150.5, models.FromCents(15050))
	assert.Equal(t, 0.0, models.FromCents(0))
}

func TestInvalidError(t *testing.T) {
	err := models.Invalid("account_closed", "account is closed")

	assert.EqualError(t, err, "account_closed: account is closed")
	assert.True(t, models.IsInvalid(err))
	assert.Equal(t, "account_closed", models.InvalidCode(err))
	assert.False(t, models.IsInvalid(errors.New("other")))
	assert.Equal(t, "", models.InvalidCode(errors.New("other")))
	assert.False(t, models.IsInvalid(models.ErrNotFound))
}
