package unit

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/stretchr/testify/assert"
)

func TestMapWriteErrorFlagsAmbiguousWrites(t *testing.T) {
	cases := map[string]error{
		"write timeout pointer": &gocql.RequestErrWriteTimeout{Consistency: gocql.Quorum, Received: 1, BlockFor: 2, WriteType: "SIMPLE"},
		"write timeout value":   gocql.RequestErrWriteTimeout{Consistency: gocql.Quorum, Received: 1, BlockFor: 2, WriteType: "SIMPLE"},
		"unavailable pointer":   &gocql.RequestErrUnavailable{Consistency: gocql.Quorum, Required: 2, Alive: 1},
		"unavailable value":     gocql.RequestErrUnavailable{Consistency: gocql.Quorum, Required: 2, Alive: 1},
		"wrapped":               fmt.Errorf("batch: %w", &gocql.RequestErrWriteTimeout{}),
		"timeout no response":   gocql.ErrTimeoutNoResponse,
		"context deadline":      fmt.Errorf("x: %w", context.DeadlineExceeded),
		"context cancelled":     context.Canceled,
	}

	for name, err := range cases {
		mapped := database.MapWriteError(err)
		assert.ErrorIs(t, mapped, models.ErrAmbiguousWrite, name)
		assert.ErrorContains(t, mapped, "ambiguous write", name)
	}
}

func TestMapWriteErrorLeavesOtherErrorsUntouched(t *testing.T) {
	boom := errors.New("boom")
	other := errors.New("other")

	assert.Same(t, boom, database.MapWriteError(boom))
	assert.NoError(t, database.MapWriteError(nil))
	assert.NotErrorIs(t, database.MapWriteError(&gocql.RequestErrReadTimeout{}), models.ErrAmbiguousWrite)
	assert.Same(t, gocql.ErrNotFound, database.MapWriteError(gocql.ErrNotFound))
	assert.Same(t, other, database.MapWriteError(other))
}
