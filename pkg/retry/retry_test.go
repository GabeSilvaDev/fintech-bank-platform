package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDoSucceedsOnFirstTry(t *testing.T) {
	callCount := 0
	err := Do(context.Background(), 3, time.Millisecond, func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestDoSucceedsOnThirdTry(t *testing.T) {
	callCount := 0
	err := Do(context.Background(), 3, time.Millisecond, func() error {
		callCount++
		if callCount < 3 {
			return errors.New("not ready yet")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestDoGivesUpAfterAttempts(t *testing.T) {
	callCount := 0
	expectedErr := errors.New("down 3")
	err := Do(context.Background(), 3, time.Millisecond, func() error {
		callCount++
		return expectedErr
	})

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 3, callCount)
}

func TestDoAttemptsZeroDefaultsToOne(t *testing.T) {
	callCount := 0
	expectedErr := errors.New("failed")
	err := Do(context.Background(), 0, time.Millisecond, func() error {
		callCount++
		return expectedErr
	})

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 1, callCount)
}

func TestDoAttemptsNegativeDefaultsToOne(t *testing.T) {
	callCount := 0
	expectedErr := errors.New("failed")
	err := Do(context.Background(), -5, time.Millisecond, func() error {
		callCount++
		return expectedErr
	})

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 1, callCount)
}

func TestDoContextCancelledDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	startTime := time.Now()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, 3, time.Second, func() error {
		callCount++
		return errors.New("not ready")
	})

	elapsed := time.Since(startTime)

	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 1, callCount)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestDoReturnsNilOnSuccess(t *testing.T) {
	callCount := 0
	err := Do(context.Background(), 5, time.Millisecond, func() error {
		callCount++
		if callCount == 2 {
			return nil
		}
		return errors.New("not ready")
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDoWaitsDelayBetweenAttempts(t *testing.T) {
	callTimes := []time.Duration{}
	startTime := time.Now()

	err := Do(context.Background(), 3, 50*time.Millisecond, func() error {
		callTimes = append(callTimes, time.Since(startTime))
		return errors.New("not ready")
	})

	assert.Error(t, err)
	assert.Equal(t, 3, len(callTimes))

	if len(callTimes) >= 2 {
		delay := callTimes[1] - callTimes[0]
		assert.GreaterOrEqual(t, delay, 50*time.Millisecond)
	}
	if len(callTimes) >= 3 {
		delay := callTimes[2] - callTimes[1]
		assert.GreaterOrEqual(t, delay, 50*time.Millisecond)
	}
}

func TestDoLastErrorReturned(t *testing.T) {
	errors := []error{
		errors.New("error 1"),
		errors.New("error 2"),
		errors.New("error 3"),
	}
	callCount := 0

	err := Do(context.Background(), 3, time.Millisecond, func() error {
		result := errors[callCount]
		callCount++
		return result
	})

	assert.Equal(t, errors[2], err)
}

func TestDoContextAlreadyCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	callCount := 0
	err := Do(ctx, 3, time.Millisecond, func() error {
		callCount++
		return errors.New("not ready")
	})

	assert.Equal(t, context.Canceled, err)
}

func TestDoSingleAttemptSuccess(t *testing.T) {
	callCount := 0
	err := Do(context.Background(), 1, time.Millisecond, func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestDoSingleAttemptFailure(t *testing.T) {
	callCount := 0
	expectedErr := errors.New("failed")
	err := Do(context.Background(), 1, time.Millisecond, func() error {
		callCount++
		return expectedErr
	})

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 1, callCount)
}
