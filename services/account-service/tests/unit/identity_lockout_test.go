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
	"github.com/fintech-bank-platform/pkg/domain"
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

func (h *identityHarness) failureCount(email string) int {
	row, _ := h.failures.Row(email)
	return row.Failures
}

func (h *identityHarness) onWrite(n int, change func(rows map[string]models.LoginFailure)) {
	writes := 0
	h.failures.BeforeWrite = func(rows map[string]models.LoginFailure) {
		writes++
		if writes == n {
			change(rows)
		}
	}
}

func tooManyAttempts(t *testing.T, err error) time.Duration {
	t.Helper()
	var locked *services.ErrTooManyAttempts
	require.ErrorAs(t, err, &locked)
	return locked.RetryAfter
}

func bump(email string) func(rows map[string]models.LoginFailure) {
	return func(rows map[string]models.LoginFailure) {
		row := rows[email]
		row.Failures++
		rows[email] = row
	}
}

func TestLockoutDefaultsToFiveFailuresInFifteenMinutes(t *testing.T) {
	h := newIdentityHarness(t)
	h.known(t, "ana@example.com", "correct horse")

	h.fail(t, "ana@example.com", 5)
	_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	assert.Equal(t, 15*time.Minute, tooManyAttempts(t, err))
	assert.Equal(t, 15*time.Minute, h.failures.Writes[0].TTL)
	assert.Equal(t, 5, services.DefaultLoginMaxFailures)
	assert.Equal(t, 15*time.Minute, services.DefaultLoginLockoutWindow)
}

func TestLockoutReservesTheAttemptBeforeComparing(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.hasher.Comparisons = nil
	h.failures.BeforeWrite = func(map[string]models.LoginFailure) {
		assert.Empty(t, h.hasher.Comparisons)
	}

	h.fail(t, "ana@example.com", 1)

	require.Len(t, h.failures.Writes, 1)
	assert.Len(t, h.hasher.Comparisons, 1)
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

func TestLockoutAllowsExactlyTheMaximumOfWrongAttempts(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")

	h.fail(t, "ana@example.com", 3)
	assert.Len(t, h.hasher.Comparisons, 3)
	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

	assert.Equal(t, 10*time.Minute, tooManyAttempts(t, err))
	assert.Len(t, h.hasher.Comparisons, 3)
	assert.Equal(t, 3, h.failureCount("ana@example.com"))
	assert.Len(t, h.failures.Writes, 3)

	other := newLockoutHarness(t, lockout)
	otherID := other.known(t, "ana@example.com", "correct horse")
	other.fail(t, "ana@example.com", 2)
	verified, err := other.service.Verify(context.Background(), "ana@example.com", "correct horse")
	require.NoError(t, err)
	assert.Equal(t, otherID, verified)
}

func TestLockoutRejectsTheRightPasswordWithoutComparingOrLookingItUp(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 3)
	h.repo.GetErr = errors.New("identity must not be looked up")
	h.hasher.Comparisons = nil
	writes := len(h.failures.Writes)

	verified, err := h.service.Verify(context.Background(), " ANA@example.com ", "correct horse")

	tooManyAttempts(t, err)
	assert.Equal(t, uuid.Nil, verified)
	assert.Empty(t, h.hasher.Comparisons)
	assert.Len(t, h.failures.Writes, writes)
	assert.Empty(t, h.failures.Cleared)
}

func TestLockoutCountsUnknownEmailsAndLocksThemTheSameWay(t *testing.T) {
	known := newLockoutHarness(t, lockout)
	known.known(t, "ana@example.com", "correct horse")
	unknown := newLockoutHarness(t, lockout)

	known.fail(t, "ana@example.com", 3)
	unknown.fail(t, "ghost@example.com", 3)
	assert.Equal(t, 3, unknown.failureCount("ghost@example.com"))

	known.hasher.Comparisons = nil
	unknown.hasher.Comparisons = nil
	_, knownErr := known.service.Verify(context.Background(), "ana@example.com", "correct horse")
	_, unknownErr := unknown.service.Verify(context.Background(), "ghost@example.com", "correct horse")

	assert.Equal(t, tooManyAttempts(t, knownErr), tooManyAttempts(t, unknownErr))
	assert.Equal(t, knownErr.Error(), unknownErr.Error())
	assert.Empty(t, known.hasher.Comparisons)
	assert.Empty(t, unknown.hasher.Comparisons)
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
	assert.Equal(t, 3, h.failureCount("not-an-email"))
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
	assert.Equal(t, 1, h.failureCount("ana@example.com"))
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
	_, recorded := h.failures.Row("ana@example.com")
	assert.False(t, recorded)
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
	assert.Equal(t, 2, h.failureCount("ana@example.com"))
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

func TestLockoutClearsTheCountAfterASuccessfulLogin(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	userID := h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 2)

	verified, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	require.NoError(t, err)
	assert.Equal(t, userID, verified)
	assert.Equal(t, []string{"ana@example.com"}, h.failures.Cleared)
	_, ok := h.failures.Row("ana@example.com")
	assert.False(t, ok)
	h.fail(t, "ana@example.com", 3)
	_, err = h.service.Verify(context.Background(), "ana@example.com", "correct horse")
	tooManyAttempts(t, err)
}

func TestLockoutClearsFailuresRecordedConcurrentlyWithASuccess(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.onWrite(1, func(rows map[string]models.LoginFailure) {
		rows["ana@example.com"] = models.LoginFailure{Email: "ana@example.com", Failures: 2, FirstFailure: now}
	})

	_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	require.NoError(t, err)
	_, ok := h.failures.Row("ana@example.com")
	assert.False(t, ok)
}

func TestLockoutFailsClosedWhenTheFailureStoreFails(t *testing.T) {
	storeErr := errors.New("cassandra down")
	for name, tc := range map[string]struct {
		seeded bool
		broken func(*tests.FakeLoginFailureRepo)
	}{
		"get":     {seeded: true, broken: func(f *tests.FakeLoginFailureRepo) { f.GetErr = storeErr }},
		"create":  {seeded: false, broken: func(f *tests.FakeLoginFailureRepo) { f.CreateErr = storeErr }},
		"replace": {seeded: true, broken: func(f *tests.FakeLoginFailureRepo) { f.ReplaceErr = storeErr }},
	} {
		h := newLockoutHarness(t, lockout)
		h.known(t, "ana@example.com", "correct horse")
		if tc.seeded {
			h.failures.Rows["ana@example.com"] = models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: now}
		}
		tc.broken(h.failures)

		verified, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

		assert.ErrorIs(t, err, services.ErrBusy, name)
		assert.Equal(t, uuid.Nil, verified, name)
		assert.Empty(t, h.hasher.Comparisons, name)
	}
}

func TestLockoutTreatsAmbiguousWritesAsContention(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.failures.CreateErr = domain.ErrAmbiguousWrite

	_, err := h.service.Verify(context.Background(), "ghost@example.com", "correct horse")

	assert.Equal(t, time.Second, tooManyAttempts(t, err))
	assert.Empty(t, h.hasher.Comparisons)
}

func TestLockoutIgnoresAFailedClear(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	userID := h.known(t, "ana@example.com", "correct horse")
	h.failures.ClearErr = errors.New("cassandra down")

	verified, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	require.NoError(t, err)
	assert.Equal(t, userID, verified)
}

func TestLockoutRetriesALostRace(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.known(t, "ana@example.com", "correct horse")
	h.fail(t, "ana@example.com", 1)
	h.onWrite(1, bump("ana@example.com"))

	h.fail(t, "ana@example.com", 1)

	require.Len(t, h.failures.Writes, 3)
	assert.False(t, h.failures.Writes[1].Applied)
	assert.True(t, h.failures.Writes[2].Applied)
	assert.Equal(t, 3, h.failureCount("ana@example.com"))
}

func TestLockoutRetriesACreateThatLostTheRace(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.onWrite(1, func(rows map[string]models.LoginFailure) {
		rows["ghost@example.com"] = models.LoginFailure{Email: "ghost@example.com", Failures: 1, FirstFailure: now}
	})

	h.fail(t, "ghost@example.com", 1)

	require.Len(t, h.failures.Writes, 2)
	assert.Equal(t, "create", h.failures.Writes[0].Kind)
	assert.False(t, h.failures.Writes[0].Applied)
	assert.Equal(t, "replace", h.failures.Writes[1].Kind)
	assert.True(t, h.failures.Writes[1].Applied)
	assert.Equal(t, 2, h.failureCount("ghost@example.com"))
}

func TestLockoutGivesUpAfterFiveLostRacesWithoutComparing(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.failures.Rows["ana@example.com"] = models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: now}
	h.failures.BeforeWrite = func(rows map[string]models.LoginFailure) {
		row := rows["ana@example.com"]
		row.FirstFailure = row.FirstFailure.Add(time.Millisecond)
		rows["ana@example.com"] = row
	}
	h.hasher.Comparisons = nil

	_, err := h.service.Verify(context.Background(), "ana@example.com", "correct horse")

	assert.Equal(t, time.Second, tooManyAttempts(t, err))
	require.Len(t, h.failures.Writes, 5)
	for _, write := range h.failures.Writes {
		assert.False(t, write.Applied)
	}
	assert.Equal(t, 5, h.failures.GetCalls)
	assert.Empty(t, h.hasher.Comparisons)
	assert.Equal(t, 1, h.failureCount("ana@example.com"))
}

func TestLockoutReleasesTheReservationWhenTheLookupFails(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.fail(t, "ana@example.com", 1)
	h.repo.GetErr = errors.New("cassandra down")

	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

	assert.EqualError(t, err, "cassandra down")
	assert.Equal(t, 1, h.failureCount("ana@example.com"))
	last := h.failures.Writes[len(h.failures.Writes)-1]
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 2, FirstFailure: now}, *last.Current)
	assert.Equal(t, models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: now}, last.Next)
	assert.Equal(t, 10*time.Minute, last.TTL)
}

func TestLockoutReleaseRetriesALostRace(t *testing.T) {
	h := newLockoutHarness(t, lockout)
	h.repo.GetErr = errors.New("cassandra down")
	h.onWrite(2, bump("ana@example.com"))

	_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

	assert.EqualError(t, err, "cassandra down")
	require.Len(t, h.failures.Writes, 3)
	assert.False(t, h.failures.Writes[1].Applied)
	assert.True(t, h.failures.Writes[2].Applied)
	assert.Equal(t, 1, h.failureCount("ana@example.com"))
}

func TestLockoutReleaseGivesUpSafely(t *testing.T) {
	later := now.Add(time.Minute)
	for name, tc := range map[string]struct {
		change func(h *identityHarness, rows map[string]models.LoginFailure)
		want   models.LoginFailure
	}{
		"reread fails": {
			change: func(h *identityHarness, rows map[string]models.LoginFailure) {
				bump("ana@example.com")(rows)
				h.failures.GetErr = errors.New("cassandra down")
			},
			want: models.LoginFailure{Email: "ana@example.com", Failures: 2, FirstFailure: now},
		},
		"window restarted": {
			change: func(_ *identityHarness, rows map[string]models.LoginFailure) {
				rows["ana@example.com"] = models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: later}
			},
			want: models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: later},
		},
		"window over": {
			change: func(h *identityHarness, rows map[string]models.LoginFailure) {
				bump("ana@example.com")(rows)
				h.clock.T = now.Add(10 * time.Minute)
			},
			want: models.LoginFailure{Email: "ana@example.com", Failures: 2, FirstFailure: now},
		},
		"already released": {
			change: func(_ *identityHarness, rows map[string]models.LoginFailure) {
				rows["ana@example.com"] = models.LoginFailure{Email: "ana@example.com", Failures: 0, FirstFailure: now}
			},
			want: models.LoginFailure{Email: "ana@example.com", Failures: 0, FirstFailure: now},
		},
		"write fails": {
			change: func(h *identityHarness, _ map[string]models.LoginFailure) {
				h.failures.ReplaceErr = errors.New("cassandra down")
			},
			want: models.LoginFailure{Email: "ana@example.com", Failures: 1, FirstFailure: now},
		},
	} {
		h := newLockoutHarness(t, lockout)
		h.repo.GetErr = errors.New("identity store down")
		h.onWrite(2, func(rows map[string]models.LoginFailure) { tc.change(h, rows) })

		_, err := h.service.Verify(context.Background(), "ana@example.com", "wrong horse")

		assert.EqualError(t, err, "identity store down", name)
		row, _ := h.failures.Row("ana@example.com")
		assert.Equal(t, tc.want, row, name)
	}
}

func TestTooManyAttemptsErrorMentionsTheWait(t *testing.T) {
	err := &services.ErrTooManyAttempts{RetryAfter: 90 * time.Second}

	assert.Equal(t, "too many failed attempts, retry after 1m30s", err.Error())
}
