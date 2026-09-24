package unit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var lockout = contracts.LockoutConfig{MaxFailures: 3, Window: 10 * time.Minute}

func (h *identityHarness) known(t *testing.T, email, password string) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	h.repo.Identities[email] = &models.Identity{Email: email, UserID: userID, PasswordHash: "hashed:" + password}
	return userID
}

func (h *identityHarness) fail(t *testing.T, email string, times int) {
	t.Helper()
	for i := 0; i < times; i++ {
		_, err := h.service.Verify(context.Background(), email, "wrong horse")
		require.ErrorIs(t, err, services.ErrInvalidCredentials)
	}
}

func (h *identityHarness) dummyHash() string {
	return "hashed:" + h.hasher.Hashed[0]
}

func tooManyAttempts(t *testing.T, err error) time.Duration {
	t.Helper()
	var locked *services.ErrTooManyAttempts
	require.ErrorAs(t, err, &locked)
	return locked.RetryAfter
}

func TestLockoutDefaultsToFiveFailuresInFifteenMinutes(t *testing.T) {
	h := newIdentityHarness(t)
	h.known(t, "ana@example.com", "correct horse")

	h.fail(t, "ana@example.com", 4)
	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")
	require.ErrorIs(t, err, services.ErrInvalidCredentials)

	_, err = h.service.Verify(context.Background(), "ana@example.com", "correct horse")
	assert.Equal(t, 15*time.Minute, tooManyAttempts(t, err))
	assert.Equal(t, 15*time.Minute, h.failures.Writes[0].TTL)
	assert.Equal(t, services.DefaultLoginMaxFailures, 5)
	assert.Equal(t, services.DefaultLoginLockoutWindow, 15*time.Minute)
}

func TestLockoutStartsExactlyAtTheMaximum(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")

	h.fail(t, "ana@example.com", 2)
	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")
	require.ErrorIs(t, err, services.ErrInvalidCredentials)
	row, _ := h.failures.Row("ana@example.com")
	assert.Equal(t, 3, row.Failures)

	_, err = h.service.Verify(context.Background(), "ana@example.com", "wrong horse")
	assert.Equal(t, 10*time.Minute, tooManyAttempts(t, err))
	row, _ = h.failures.Row("ana@example.com")
	assert.Equal(t, 3, row.Failures)

	other := newLockoutHarness(t, lockout)
	otherID := other.known(t, "ana@example.com", "correct horse")
	other.fail(t, "ana@example.com", 2)
	verified, err := other.service.Verify(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)
	assert.Equal(t, otherID, verified)
}

func TestLockoutRecordsTheFirstFailureAndIncrementsWithinTheWindow(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")

	h.fail(t, "ana@example.com", 1)
	h.clock.T = now.Add(90*time.Second + 500*time.Millisecond)
	h.fail(t, "ana@example.com", 1)

	require.Len(t, h.failures.Writes, 2)
	first := h.failures.Writes[0]
	assert.Equal(t, "create", first.Kind)
	assert.True(t, first.Applied)
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: now}, first.Next)
	assert.Equal(t, 10*time.Minute, first.TTL)
	second := h.failures.Writes[1]
	assert.Equal(t, "replace", second.Kind)
	assert.True(t, second.Applied)
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: now}, *second.Current)
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 2, FirstFailure: now}, second.Next)
	assert.Equal(t, 10*time.Minute-90*time.Second-500*time.Millisecond, second.TTL)
}

func TestLockoutRejectsTheRightPasswordWithoutLookingItUp(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 3)
	h.repo.GetErr = errors.New("identity must not be looked up")
	h.hasher.Comparisons = nil
	writes := len(h.failures.Writes)

	verified, err := h.service.Verify(context.Background(), " ANA@example.com ", "correct horse")

	tooManyAttempts(t, err)
	assert.Equal(t, uuid.Nil, verified)
	assert.Equal(t, []tests.Comparison{{Hash: h.dummyHash(), Password: "correct horse"}}, h.hasher.Comparisons)
	assert.Len(t, h.failures.Writes, writes)
	assert.Empty(t, h.failures.Cleared)
}

func TestLockoutCountsUnknownEmailsAndLocksThemTheSameWay(t *testing.T) {
	known := newLockoutHarness(t, lockout)
	known.known(t, "ana@example.com", "correct horse")
	unknown := newLockoutHarness(t, lockout)

	known.fail(t, "ana@example.com", 3)
	unknown.fail(t, "ghost@example.com", 3)
	row, ok := unknown.failures.Row("ghost@example.com")
	require.True(t, ok)
	assert.Equal(t, 3, row.Failures)

	known.hasher.Comparisons = nil
	unknown.hasher.Comparisons = nil
	_, knownErr := known.service.Verify(context.Background(), "ana@example.com", "correct horse")
	_, unknownErr := unknown.service.Verify(context.Background(), "ghost@example.com", "correct horse")

	assert.Equal(t, tooManyAttempts(t, knownErr), tooManyAttempts(t, unknownErr))
	assert.Equal(t, knownErr.Error(), unknownErr.Error())
	assert.Equal(t, []tests.Comparison{{Hash: known.dummyHash(), Password: "correct horse"}}, known.hasher.Comparisons)
	assert.Equal(t, []tests.Comparison{{Hash: unknown.dummyHash(), Password: "correct horse"}}, unknown.hasher.Comparisons)
	assert.Equal(t, known.failures.GetCalls, unknown.failures.GetCalls)
}

func TestLockoutCountsUnusableEmailsUnderTheirNormalisedForm(t *testing.T) {
	h := newLockoutHarness(t, lockout)

	for i := 0; i < 3; i++ {
		_, err := h.service.Verify(context.Background(), "  Not-An-Email ", "correct horse")
		require.ErrorIs(t, err, services.ErrInvalidCredentials)
	}
	_, err := h.service.Verify(context.Background(), "not-an-email", "correct horse")

	tooManyAttempts(t, err)
	row, ok := h.failures.Row("not-an-email")
	require.True(t, ok)
	assert.Equal(t, 3, row.Failures)
}

func TestLockoutSkipsEmptyEmails(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.failures.GetErr = errors.New("failures must not be read")

	for _, email := range []string{"", "   "} {
		_, err := h.service.Verify(context.Background(), email, "correct horse")
		require.ErrorIs(t, err, services.ErrInvalidCredentials)
	}

	assert.Zero(t, h.failures.GetCalls)
	assert.Empty(t, h.failures.Writes)
	assert.Len(t, h.hasher.Comparisons, 2)
}

func TestLockoutCountsPasswordsBeyondTheBcryptLimit(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	long := strings.Repeat("a", 73)
	h.known(t, "ana@example.com", long)

	_, err := h.service.Verify(context.Background(), "ana@example.com", long)

	require.ErrorIs(t, err, services.ErrInvalidCredentials)
	row, _ := h.failures.Row("ana@example.com")
	assert.Equal(t, 1, row.Failures)
}

func TestLockoutEndsWithTheWindow(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	userID := h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 3)

	h.clock.T = now.Add(10*time.Minute - time.Nanosecond)
	_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")
	assert.Equal(t, time.Second, tooManyAttempts(t, err))

	h.clock.T = now.Add(10 * time.Minute)
	verified, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)
	assert.Equal(t, userID, verified)
}

func TestLockoutRestartsTheCountOnceTheWindowHasEnded(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 3)
	later := now.Add(10*time.Minute + time.Second)
	h.clock.T = later

	h.fail(t, "ana@example.com", 1)

	last := h.failures.Writes[len(h.failures.Writes)-1]
	assert.Equal(t, "replace", last.Kind)
	assert.True(t, last.Applied)
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 3, FirstFailure: now}, *last.Current)
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: later}, last.Next)
	assert.Equal(t, 10*time.Minute, last.TTL)
	h.fail(t, "ana@example.com", 1)
	row, _ := h.failures.Row("ana@example.com")
	assert.Equal(t, 2, row.Failures)
}

func TestLockoutRoundsRetryAfterUpToWholeSeconds(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{0, 10 * time.Minute},
		{time.Minute, 9 * time.Minute},
		{4*time.Minute + 200*time.Millisecond, 6 * time.Minute},
		{9*time.Minute + 59*time.Second + 999*time.Millisecond, time.Second},
	} {
		h := newLockoutHarness(t, lockout)
		h.failures.Rows["ana@example.com"] = models.LoginFailure{Email: "ana@example.com", Failures: 3, FirstFailure: now}
		h.clock.T = now.Add(tc.elapsed)

		_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

		assert.Equal(t, tc.want, tooManyAttempts(t, err), tc.elapsed)
	}
}

func TestLockoutResetsAfterASuccessfulLogin(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	userID := h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 2)

	verified, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	require.NoError(t, err)
	assert.Equal(t, userID, verified)
	assert.Equal(t, []string{"ana@example.com"}, h.failures.Cleared)
	_, ok := h.failures.Row("ana@example.com")
	assert.False(t, ok)
	h.fail(t, "ana@example.com", 2)
	_, err = h.service.Verify(context.Background(), "ana@example.com", "correct horse")
	assert.NoError(t, err)
}

func TestLockoutDoesNotClearWhenNothingWasRecorded(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")

	_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	require.NoError(t, err)
	assert.Empty(t, h.failures.Cleared)
}

func TestLockoutIgnoresFailureStoreErrors(t *testing.T) {
	storeErr := errors.New("cassandra down")
	for name, broken := range map[string]func(*tests.FakeLoginFailureRepo){
		"get":     func(f *tests.FakeLoginFailureRepo) { f.GetErr = storeErr },
		"create":  func(f *tests.FakeLoginFailureRepo) { f.CreateErr = storeErr },
		"replace": func(f *tests.FakeLoginFailureRepo) { f.ReplaceErr = storeErr },
		"clear":   func(f *tests.FakeLoginFailureRepo) { f.ClearErr = storeErr },
	} {
		h := newLockoutHarness(t, lockout)
		userID := h.known(t, "ana@example.com", "correct horse")
		h.fail(t, "ana@example.com", 1)
		broken(h.failures)

		_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")
		assert.ErrorIs(t, err, services.ErrInvalidCredentials, name)
		_, err = h.service.Verify(context.Background(), "ghost@example.com", "wrong horse")
		assert.ErrorIs(t, err, services.ErrInvalidCredentials, name)

		verified, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")
		assert.NoError(t, err, name)
		assert.Equal(t, userID, verified, name)
	}
}

func TestLockoutRetriesALostRace(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 1)
	raced := false
	h.failures.BeforeWrite = func(rows map[string]models.LoginFailure) {
		if !raced {
			raced = true
			row := rows["ana@example.com"]
			row.Failures++
			rows["ana@example.com"] = row
		}
	}

	h.fail(t, "ana@example.com", 1)

	require.Len(t, h.failures.Writes, 3)
	assert.False(t, h.failures.Writes[1].Applied)
	assert.True(t, h.failures.Writes[2].Applied)
	row, _ := h.failures.Row("ana@example.com")
	assert.Equal(t, 3, row.Failures)
}

func TestLockoutRetriesACreateThatLostTheRace(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.failures.BeforeWrite = func(rows map[string]models.LoginFailure) {
		if _, ok := rows["ghost@example.com"]; !ok {
			rows["ghost@example.com"] = models.LoginFailure{Email: "ghost@example.com", Failures: 1, FirstFailure: now}
		}
	}

	h.fail(t, "ghost@example.com", 1)

	require.Len(t, h.failures.Writes, 2)
	assert.Equal(t, "create", h.failures.Writes[0].Kind)
	assert.False(t, h.failures.Writes[0].Applied)
	assert.Equal(t, "replace", h.failures.Writes[1].Kind)
	assert.True(t, h.failures.Writes[1].Applied)
	row, _ := h.failures.Row("ghost@example.com")
	assert.Equal(t, 2, row.Failures)
}

func TestLockoutGivesUpAfterThreeLostRaces(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 1)
	h.failures.BeforeWrite = func(rows map[string]models.LoginFailure) {
		row := rows["ana@example.com"]
		row.FirstFailure = row.FirstFailure.Add(time.Millisecond)
		rows["ana@example.com"] = row
	}
	gets := h.failures.GetCalls

	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

	assert.ErrorIs(t, err, services.ErrInvalidCredentials)
	require.Len(t, h.failures.Writes, 4)
	for _, write := range h.failures.Writes[1:] {
		assert.False(t, write.Applied)
	}
	assert.Equal(t, gets+3, h.failures.GetCalls)
	row, _ := h.failures.Row("ana@example.com")
	assert.Equal(t, 1, row.Failures)
}

func TestLockoutDoesNotCountRepositoryErrors(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.repo.GetErr = errors.New("cassandra down")

	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

	assert.EqualError(t, err, "cassandra down")
	assert.Empty(t, h.failures.Writes)
}

func TestTooManyAttemptsErrorMentionsTheWait(t *testing.T) {
	err := &services.ErrTooManyAttempts{RetryAfter: 90 * time.Second}

	assert.Equal(t, "too many failed attempts, retry after 1m30s", err.Error())
}
