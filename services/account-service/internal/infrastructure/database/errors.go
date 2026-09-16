package database

import (
	"errors"
	"fmt"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
)

func MapWriteError(err error) error {
	if isAmbiguous(err) {
		return fmt.Errorf("%w: %v", models.ErrAmbiguousWrite, err)
	}
	return err
}

func isAmbiguous(err error) bool {
	var (
		writeTimeoutPtr *gocql.RequestErrWriteTimeout
		writeTimeout    gocql.RequestErrWriteTimeout
		unavailablePtr  *gocql.RequestErrUnavailable
		unavailable     gocql.RequestErrUnavailable
	)
	return errors.As(err, &writeTimeoutPtr) ||
		errors.As(err, &writeTimeout) ||
		errors.As(err, &unavailablePtr) ||
		errors.As(err, &unavailable)
}
