package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToCents(t *testing.T) {
	cents, err := ToCents(150.5)
	assert.NoError(t, err)
	assert.Equal(t, int64(15050), cents)

	cents, err = ToCents(0.1)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), cents)

	_, err = ToCents(0)
	assert.Equal(t, "invalid_amount", InvalidCode(err))

	_, err = ToCents(-5)
	assert.True(t, IsInvalid(err))

	_, err = ToCents(1.005)
	assert.True(t, IsInvalid(err))
}

func TestInvalidError(t *testing.T) {
	err := Invalid("account_closed", "account is closed")

	assert.EqualError(t, err, "account_closed: account is closed")
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "account_closed", InvalidCode(err))
	assert.False(t, IsInvalid(errors.New("other")))
	assert.Equal(t, "", InvalidCode(errors.New("other")))
	assert.False(t, IsInvalid(ErrNotFound))
	assert.NotEqual(t, ErrConflict, ErrAmbiguousWrite)
}
