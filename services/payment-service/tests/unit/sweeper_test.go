package unit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/internal/contracts"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func sweeperConfig() contracts.SweeperConfig {
	return contracts.SweeperConfig{Enabled: true, Interval: time.Minute, StaleAfter: 5 * time.Minute, Batch: 10}
}

func TestSweeperRunOnceResendsOnlyStaleRecords(t *testing.T) {
	h := newHarness()
	stale := h.stored(models.MethodPix, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)
	fresh := h.stored(models.MethodPix, models.StatusPending)
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
	assert.Equal(t, events.EventTypes.DebitAccount, published.Event.Type)
	debit := published.Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, models.Reference(stale.ID), debit.Reference)
}

func TestSweeperRunOnceLogsAndSkipsPublishFailures(t *testing.T) {
	h := newHarness()
	stale := h.stored(models.MethodPix, models.StatusPending)
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
	stale := h.stored(models.MethodPix, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)
	h.repo.TouchResults = []tests.TransitionResult{{Err: errors.New("cas conflict")}}

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

func TestSweeperRunOnceLogsRecordsWithNoStepToResend(t *testing.T) {
	h := newHarness()
	stale := h.stored(models.MethodPix, models.StatusCompleted)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Contains(t, logs.String(), "stale payment has no step to re-send")
	assert.Contains(t, logs.String(), stale.ID.String())
}

func TestSweeperRunOnceTreatsSubmittedPendingAsResentWithoutWarning(t *testing.T) {
	h := newHarness()
	stale := h.stored(models.MethodBoleto, models.StatusSubmitted)
	stale.ExternalID = "boleto_1"
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)
	h.gateway.Submissions = []models.Submission{{ExternalID: "boleto_1", Status: models.SubmissionPending}}

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Empty(t, publisher.Published)
	assert.NotContains(t, logs.String(), "stale payment has no step to re-send")
	assert.Contains(t, logs.String(), "stale payment re-sent")
	assert.Contains(t, logs.String(), stale.ID.String())
}

func TestSweeperRunOnceSkipsRecordsTouchedByAnotherSweeperSilently(t *testing.T) {
	h := newHarness()
	stale := h.stored(models.MethodPix, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)
	h.repo.TouchResults = []tests.TransitionResult{{Applied: false}}

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Empty(t, logs.String())
}

type cancelingPublisher struct {
	inner  *tests.FakePublisher
	cancel context.CancelFunc
}

func (p *cancelingPublisher) Publish(ctx context.Context, topic, key string, event *events.Event) error {
	err := p.inner.Publish(ctx, topic, key, event)
	p.cancel()
	return err
}

func TestSweeperRunOnceStopsBetweenRecordsWhenContextCancelled(t *testing.T) {
	h := newHarness()
	first := h.stored(models.MethodPix, models.StatusPending)
	first.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(first)
	second := h.stored(models.MethodPix, models.StatusPending)
	second.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(second)

	inner := &tests.FakePublisher{}
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &cancelingPublisher{inner: inner, cancel: cancel}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, count)
	assert.Len(t, inner.Published, 1)
	assert.NotContains(t, logs.String(), "reconciliation failed")
}

func TestSweeperRunOnceStopsImmediatelyWhenContextAlreadyCancelled(t *testing.T) {
	h := newHarness()
	stale := h.stored(models.MethodPix, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	publisher := &tests.FakePublisher{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	count, err := sweeper.RunOnce(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
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
	stale := h.stored(models.MethodPix, models.StatusPending)
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
