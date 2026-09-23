package cassandra

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestMapWriteErrorFlagsAmbiguousWrites(t *testing.T) {
	cases := map[string]error{
		"write timeout pointer":     &gocql.RequestErrWriteTimeout{Consistency: gocql.Quorum, Received: 1, BlockFor: 2, WriteType: "SIMPLE"},
		"write timeout value":       gocql.RequestErrWriteTimeout{Consistency: gocql.Quorum, Received: 1, BlockFor: 2, WriteType: "SIMPLE"},
		"unavailable pointer":       &gocql.RequestErrUnavailable{Consistency: gocql.Quorum, Required: 2, Alive: 1},
		"unavailable value":         gocql.RequestErrUnavailable{Consistency: gocql.Quorum, Required: 2, Alive: 1},
		"cas write unknown pointer": &gocql.RequestErrCASWriteUnknown{Consistency: gocql.Serial, Received: 1, BlockFor: 2},
		"cas write unknown value":   gocql.RequestErrCASWriteUnknown{Consistency: gocql.Serial, Received: 1, BlockFor: 2},
		"wrapped cas write unknown": fmt.Errorf("lwt: %w", &gocql.RequestErrCASWriteUnknown{}),
		"wrapped":                   fmt.Errorf("batch: %w", &gocql.RequestErrWriteTimeout{}),
		"timeout no response":       gocql.ErrTimeoutNoResponse,
		"context deadline":          fmt.Errorf("x: %w", context.DeadlineExceeded),
		"context cancelled":         context.Canceled,
	}

	for name, err := range cases {
		mapped := MapWriteError(err)
		assert.ErrorIs(t, mapped, domain.ErrAmbiguousWrite, name)
		assert.ErrorContains(t, mapped, "ambiguous write", name)
	}
}

func TestMapWriteErrorLeavesOtherErrorsUntouched(t *testing.T) {
	boom := errors.New("boom")
	other := errors.New("other")

	assert.Same(t, boom, MapWriteError(boom))
	assert.NoError(t, MapWriteError(nil))
	assert.NotErrorIs(t, MapWriteError(&gocql.RequestErrReadTimeout{}), domain.ErrAmbiguousWrite)
	assert.Same(t, gocql.ErrNotFound, MapWriteError(gocql.ErrNotFound))
	assert.Same(t, other, MapWriteError(other))
}
