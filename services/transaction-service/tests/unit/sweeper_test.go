package unit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/internal/contracts"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/stretchr/testify/assert"
)

func sweeperConfig() contracts.SweeperConfig {
	return contracts.SweeperConfig{Enabled: true, Interval: time.Minute, StaleAfter: 5 * time.Minute, Batch: 10}
}

func TestSweeperRunOnceResendsOnlyStaleRecords(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)
	fresh := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.Put(fresh)

	publisher := &tests.FakePublisher{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Len(t, publisher.Published, 1)
	published := publisher.Published[0]
	assert.Equal(t, events.Topics.AccountCommands, published.Topic)
	assert.Equal(t, stale.AccountID.String(), published.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, published.Event.Type)
	credit := published.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, stale.ID.String(), credit.Reference)
}

func TestSweeperRunOnceLogsAndSkipsPublishFailures(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	publisher := &tests.FakePublisher{Err: errors.New("kafka down")}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Contains(t, logs.String(), "reconciliation publish failed")
	assert.Contains(t, logs.String(), stale.ID.String())
}

func TestSweeperRunOnceReturnsListError(t *testing.T) {
	h := newHarness()
	h.repo.StaleErr = errors.New("cassandra down")
	publisher := &tests.FakePublisher{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard}))

	count, err := sweeper.RunOnce(context.Background())

	assert.EqualError(t, err, "cassandra down")
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
}

func TestSweeperRunOnceLogsReconcileFailures(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)
	h.repo.TransitionResults = []tests.TransitionResult{{Err: errors.New("cas conflict")}}

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Contains(t, logs.String(), "reconciliation failed")
	assert.Contains(t, logs.String(), stale.ID.String())
}

func TestSweeperRunOnceSkipsRecordsWithNoMessages(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeTransfer, models.StatusDebited)
	stale.CounterpartyID = nil
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Empty(t, logs.String())
}

func TestSweeperRunLogsSweepErrors(t *testing.T) {
	h := newHarness()
	h.repo.StaleErr = errors.New("cassandra down")

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	cfg := contracts.SweeperConfig{Enabled: true, Interval: time.Millisecond, StaleAfter: 5 * time.Minute, Batch: 10}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, cfg, logger.New(logger.Config{Output: logs}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweeper.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}

	assert.Contains(t, logs.String(), "reconciliation sweep failed")
}

func TestSweeperRunPublishesAtLeastOnceAndStopsOnCancel(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	publisher := &tests.FakePublisher{}
	cfg := contracts.SweeperConfig{Enabled: true, Interval: time.Millisecond, StaleAfter: 5 * time.Minute, Batch: 10}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, cfg, logger.New(logger.Config{Output: io.Discard}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweeper.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}

	assert.NotEmpty(t, publisher.Published)
}
