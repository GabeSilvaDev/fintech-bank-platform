package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
