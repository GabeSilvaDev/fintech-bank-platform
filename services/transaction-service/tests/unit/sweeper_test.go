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
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/internal/contracts"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sweeperConfig() contracts.SweeperConfig {
	return contracts.SweeperConfig{Enabled: true, Interval: time.Minute, StaleAfter: 5 * time.Minute, MaxAge: 24 * time.Hour, Batch: 10}
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
	assert.Contains(t, logs.String(), "stale transaction has no step to re-send")
	assert.Contains(t, logs.String(), stale.ID.String())
}

func TestSweeperRunOnceSkipsRecordsTouchedByAnotherSweeperSilently(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
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
	first := h.pending(models.TypeDeposit, models.StatusPending)
	first.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(first)
	second := h.pending(models.TypeDeposit, models.StatusPending)
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
	stale := h.pending(models.TypeDeposit, models.StatusPending)
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
	cfg := contracts.SweeperConfig{Enabled: true, Interval: time.Millisecond, StaleAfter: 5 * time.Minute, MaxAge: 24 * time.Hour, Batch: 10}
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
	cfg := contracts.SweeperConfig{Enabled: true, Interval: time.Millisecond, StaleAfter: 5 * time.Minute, MaxAge: 24 * time.Hour, Batch: 10}
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

func (h *harness) expired(updatedAt time.Time) *models.Transaction {
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	tx.CreatedAt = now.Add(-25 * time.Hour)
	tx.UpdatedAt = updatedAt
	h.repo.Put(tx)
	return tx
}

func TestSweeperPassesTheMaxAgeToListStale(t *testing.T) {
	h := newHarness()
	sweeper := services.NewSweeper(h.service, &tests.FakePublisher{}, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard}))

	_, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []time.Duration{24 * time.Hour}, h.repo.StaleMaxAges)
}

func TestSweeperAlertsOnceInsteadOfResendingPastTheMaxAge(t *testing.T) {
	h := newHarness()
	tx := h.expired(now.Add(-2 * time.Hour))

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	require.Len(t, publisher.Published, 1)
	alert := publisher.Published[0]
	assert.Equal(t, events.Topics.TransactionDLQ, alert.Topic)
	assert.Equal(t, tx.AccountID.String(), alert.Key)
	assert.Equal(t, events.EventTypes.TransactionCommandFailed, alert.Event.Type)
	payload := alert.Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "reconciliation_exhausted", payload.ErrorCode)
	assert.Empty(t, publisher.ByTopic(events.Topics.AccountCommands))
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, now, h.repo.Touches[0].Now)
	assert.Contains(t, logs.String(), "reconciliation exhausted")
	assert.Contains(t, logs.String(), `"level":"error"`)
	assert.Contains(t, logs.String(), tx.ID.String())
	assert.Contains(t, logs.String(), `"status":"pending"`)
	assert.Contains(t, logs.String(), `"age":`)

	later := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now.Add(10 * time.Minute)}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard}))
	count, err = later.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Len(t, publisher.Published, 1)
	assert.Len(t, h.repo.Touches, 1)
}

func TestSweeperSkipsRecordsAlreadyAlertedSilently(t *testing.T) {
	h := newHarness()
	h.expired(now.Add(-30 * time.Minute))

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Empty(t, h.repo.Touches)
	assert.Empty(t, logs.String())
}

func TestSweeperSkipsAlertsTouchedByAnotherSweeperSilently(t *testing.T) {
	h := newHarness()
	h.expired(now.Add(-2 * time.Hour))
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

func TestSweeperLogsAlertTouchFailures(t *testing.T) {
	h := newHarness()
	tx := h.expired(now.Add(-2 * time.Hour))
	h.repo.TouchResults = []tests.TransitionResult{{Err: errors.New("cas timeout")}}

	publisher := &tests.FakePublisher{}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, publisher.Published)
	assert.Contains(t, logs.String(), "reconciliation failed")
	assert.Contains(t, logs.String(), tx.ID.String())
	assert.NotContains(t, logs.String(), "reconciliation exhausted")
}

func TestSweeperStillLogsTheAlertWhenItsPublishFails(t *testing.T) {
	h := newHarness()
	tx := h.expired(now.Add(-2 * time.Hour))

	publisher := &tests.FakePublisher{Err: errors.New("kafka down")}
	logs := &bytes.Buffer{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: logs}))

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Contains(t, logs.String(), "reconciliation alert publish failed")
	assert.Contains(t, logs.String(), "reconciliation exhausted")
	assert.Contains(t, logs.String(), tx.ID.String())
}

func resentCount(t *testing.T, m *metrics.Metrics, status string) float64 {
	t.Helper()
	counter := m.CounterVec(services.ResentTotalName, services.ResentTotalHelp, "status")
	return testutil.ToFloat64(counter.WithLabelValues(status))
}

func exhaustedCount(t *testing.T, m *metrics.Metrics) float64 {
	t.Helper()
	counter := m.CounterVec(services.ExhaustedTotalName, services.ExhaustedTotalHelp)
	return testutil.ToFloat64(counter.WithLabelValues())
}

func TestSweeperIncrementsResentCounterByStatusOnSuccessfulResend(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	m := metrics.New("test-sweeper-resent")
	publisher := &tests.FakePublisher{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard})).WithMetrics(m)

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, float64(1), resentCount(t, m, string(models.StatusPending)))
	assert.Equal(t, float64(0), exhaustedCount(t, m))
}

func TestSweeperDoesNotIncrementResentCounterOnPublishFailure(t *testing.T) {
	h := newHarness()
	stale := h.pending(models.TypeDeposit, models.StatusPending)
	stale.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(stale)

	m := metrics.New("test-sweeper-resent-failure")
	publisher := &tests.FakePublisher{Err: errors.New("kafka down")}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard})).WithMetrics(m)

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, float64(0), resentCount(t, m, string(models.StatusPending)))
}

func TestSweeperIncrementsExhaustedCounterWhenAlertIsPublished(t *testing.T) {
	h := newHarness()
	h.expired(now.Add(-2 * time.Hour))

	m := metrics.New("test-sweeper-exhausted")
	publisher := &tests.FakePublisher{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard})).WithMetrics(m)

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, float64(1), exhaustedCount(t, m))
}

func TestSweeperIncrementsExhaustedCounterEvenWhenAlertPublishFails(t *testing.T) {
	h := newHarness()
	h.expired(now.Add(-2 * time.Hour))

	m := metrics.New("test-sweeper-exhausted-publish-failure")
	publisher := &tests.FakePublisher{Err: errors.New("kafka down")}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard})).WithMetrics(m)

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, float64(1), exhaustedCount(t, m))
}

func TestSweeperDoesNotIncrementExhaustedCounterWhenTouchIsLost(t *testing.T) {
	h := newHarness()
	h.expired(now.Add(-2 * time.Hour))
	h.repo.TouchResults = []tests.TransitionResult{{Applied: false}}

	m := metrics.New("test-sweeper-exhausted-touch-lost")
	publisher := &tests.FakePublisher{}
	sweeper := services.NewSweeper(h.service, publisher, tests.FakeClock{T: now}, sweeperConfig(), logger.New(logger.Config{Output: io.Discard})).WithMetrics(m)

	count, err := sweeper.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, float64(0), exhaustedCount(t, m))
}
