package cassandra

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/pkg/domain"
)

func MapWriteError(err error) error {
	if isAmbiguous(err) {
		return fmt.Errorf("%w: %v", domain.ErrAmbiguousWrite, err)
	}
	return err
}

func isAmbiguous(err error) bool {
	var (
		writeTimeoutPtr *gocql.RequestErrWriteTimeout
		writeTimeout    gocql.RequestErrWriteTimeout
		unavailablePtr  *gocql.RequestErrUnavailable
		unavailable     gocql.RequestErrUnavailable
		casUnknownPtr   *gocql.RequestErrCASWriteUnknown
		casUnknown      gocql.RequestErrCASWriteUnknown
	)
	return errors.As(err, &writeTimeoutPtr) ||
		errors.As(err, &writeTimeout) ||
		errors.As(err, &unavailablePtr) ||
		errors.As(err, &unavailable) ||
		errors.As(err, &casUnknownPtr) ||
		errors.As(err, &casUnknown) ||
		errors.Is(err, gocql.ErrTimeoutNoResponse) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}
