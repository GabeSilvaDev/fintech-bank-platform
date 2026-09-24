package unit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

const (
	sessionTTL    = 2 * time.Hour
	sessionMaxAge = 24 * time.Hour
)

type sessionHarness struct {
	repo    *tests.FakeRefreshTokenRepo
	clock   *tests.FakeClock
	service *services.SessionService
}

func newSessionHarness() *sessionHarness {
	h := &sessionHarness{repo: tests.NewFakeRefreshTokenRepo(), clock: &tests.FakeClock{T: now}}
	h.service = services.NewSessionService(h.repo, h.clock, contracts.SessionConfig{RefreshTokenTTL: sessionTTL, FamilyMaxAge: sessionMaxAge})
	return h
}

func (h *sessionHarness) familyOf(token string) uuid.UUID {
	return h.repo.Tokens[digest(token)].FamilyID
}

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *sessionHarness) start(t *testing.T, userID uuid.UUID) services.Session {
	session, err := h.service.Start(context.Background(), userID)
	require.NoError(t, err)
	return session
}

func TestStartIssuesOpaqueTokenAndStoresOnlyItsHash(t *testing.T) {
	h := newSessionHarness()
	userID := uuid.New()

	session := h.start(t, userID)

	raw, err := base64.RawURLEncoding.DecodeString(session.Token)
	require.NoError(t, err)
	assert.Len(t, raw, 32)
	assert.Equal(t, now.Add(sessionTTL), session.ExpiresAt)
	require.Len(t, h.repo.Created, 1)
	stored := h.repo.Created[0]
	assert.Equal(t, digest(session.Token), stored.TokenHash)
	assert.NotContains(t, stored.TokenHash, session.Token)
	assert.Equal(t, userID, stored.UserID)
	assert.NotEqual(t, uuid.Nil, stored.FamilyID)
	assert.Equal(t, models.RefreshTokenActive, stored.Status)
	assert.Equal(t, now.Add(sessionTTL), stored.ExpiresAt)
	assert.Equal(t, now, stored.CreatedAt)
	assert.Equal(t, now, stored.FamilyCreatedAt)
	assert.Equal(t, []time.Duration{sessionTTL}, h.repo.TTLs)
}

func TestStartOpensANewFamilyEachTime(t *testing.T) {
	h := newSessionHarness()
	userID := uuid.New()

	first := h.start(t, userID)
	second := h.start(t, userID)

	assert.NotEqual(t, first.Token, second.Token)
	assert.NotEqual(t, h.repo.Created[0].FamilyID, h.repo.Created[1].FamilyID)
}

func TestStartTruncatesTimestampsToMilliseconds(t *testing.T) {
	h := newSessionHarness()
	h.clock.T = now.Add(1234567 * time.Nanosecond)

	session := h.start(t, uuid.New())

	assert.Equal(t, now.Add(time.Millisecond+sessionTTL), session.ExpiresAt)
	assert.Equal(t, now.Add(time.Millisecond), h.repo.Created[0].CreatedAt)
}

func TestStartFailsWhenTheTokenCannotBeStored(t *testing.T) {
	h := newSessionHarness()
	h.repo.CreateErr = errors.New("cassandra down")

	session, err := h.service.Start(context.Background(), uuid.New())

	assert.EqualError(t, err, "cassandra down")
	assert.Empty(t, session.Token)
}

func TestNewSessionServiceFallsBackToDefaults(t *testing.T) {
	for _, value := range []time.Duration{0, -time.Hour} {
		repo := tests.NewFakeRefreshTokenRepo()
		clock := &tests.FakeClock{T: now}
		service := services.NewSessionService(repo, clock, contracts.SessionConfig{RefreshTokenTTL: value, FamilyMaxAge: value})

		session, err := service.Start(context.Background(), uuid.New())

		require.NoError(t, err)
		assert.Equal(t, now.Add(services.DefaultRefreshTokenTTL), session.ExpiresAt)
		assert.Equal(t, []time.Duration{720 * time.Hour}, repo.TTLs)

		clock.T = now.Add(services.DefaultFamilyMaxAge - time.Hour)
		repo.Tokens[repo.Created[0].TokenHash].ExpiresAt = clock.T.Add(time.Hour)
		_, next, err := service.Rotate(context.Background(), session.Token)
		require.NoError(t, err)
		assert.Equal(t, now.Add(2160*time.Hour), next.ExpiresAt)
	}
}

func TestRotateIssuesANewTokenInTheSameFamily(t *testing.T) {
	h := newSessionHarness()
	userID := uuid.New()
	first := h.start(t, userID)
	h.clock.T = now.Add(time.Hour)

	gotUser, next, err := h.service.Rotate(context.Background(), first.Token)

	require.NoError(t, err)
	assert.Equal(t, userID, gotUser)
	assert.NotEqual(t, first.Token, next.Token)
	assert.Equal(t, now.Add(time.Hour+sessionTTL), next.ExpiresAt)
	require.Len(t, h.repo.Created, 2)
	assert.Equal(t, h.repo.Created[0].FamilyID, h.repo.Created[1].FamilyID)
	assert.Equal(t, userID, h.repo.Created[1].UserID)
	assert.Equal(t, models.RefreshTokenRotated, h.repo.StatusOf(digest(first.Token)))
	assert.Equal(t, models.RefreshTokenActive, h.repo.StatusOf(digest(next.Token)))
	assert.Equal(t, now, h.repo.Created[1].FamilyCreatedAt)
	assert.Equal(t, []time.Duration{sessionTTL - time.Hour}, h.repo.MarkTTLs)
	assert.Empty(t, h.repo.RevokedFamilies)

	_, third, err := h.service.Rotate(context.Background(), next.Token)
	require.NoError(t, err)
	assert.Equal(t, models.RefreshTokenActive, h.repo.StatusOf(digest(third.Token)))
}

func TestRotateRejectsMalformedTokensWithoutLookingThemUp(t *testing.T) {
	h := newSessionHarness()
	h.repo.GetErr = errors.New("should not be called")
	valid := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	for _, token := range []string{"", "short", strings.Repeat("a", 44), valid[:42] + "!", valid[:42] + "B", valid + "="} {
		userID, session, err := h.service.Rotate(context.Background(), token)

		assert.ErrorIs(t, err, services.ErrInvalidSession, token)
		assert.Equal(t, uuid.Nil, userID)
		assert.Empty(t, session.Token)
	}
}

func TestRotateRejectsUnknownTokens(t *testing.T) {
	h := newSessionHarness()

	_, _, err := h.service.Rotate(context.Background(), base64.RawURLEncoding.EncodeToString(make([]byte, 32)))

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Empty(t, h.repo.RevokedFamilies)
	assert.Empty(t, h.repo.Created)
}

func TestRotatePropagatesLookupErrors(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.GetErr = errors.New("cassandra down")

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "cassandra down")
}

func TestRotateRejectsExpiredTokensEvenWhenTheRowStillExists(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.clock.T = now.Add(sessionTTL)

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Equal(t, models.RefreshTokenActive, h.repo.StatusOf(digest(first.Token)))
	assert.Empty(t, h.repo.RevokedFamilies)
	assert.Len(t, h.repo.Created, 1)
}

func TestRotateAcceptsATokenJustBeforeItExpires(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.clock.T = now.Add(sessionTTL - time.Millisecond)

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.NoError(t, err)
}

func TestRotateReusingARotatedTokenRevokesTheFamily(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	_, next, err := h.service.Rotate(context.Background(), first.Token)
	require.NoError(t, err)

	userID, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Equal(t, uuid.Nil, userID)
	assert.Empty(t, session.Token)
	assert.Equal(t, []uuid.UUID{h.repo.Created[0].FamilyID}, h.repo.RevokedFamilies)
	assert.Equal(t, models.RefreshTokenRevoked, h.repo.StatusOf(digest(next.Token)))
	assert.Len(t, h.repo.Created, 2)

	_, _, err = h.service.Rotate(context.Background(), next.Token)
	assert.ErrorIs(t, err, services.ErrInvalidSession)
}

func TestRotateStopsEarlyWhenTheFamilyIsRevoked(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.Markers[h.familyOf(first.Token)] = true

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Empty(t, h.repo.RevokedFamilies)
	assert.Empty(t, h.repo.MarkTTLs)
	assert.Len(t, h.repo.Created, 1)
}

func TestRotateRevokedTokenIsRejected(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	require.NoError(t, h.service.Revoke(context.Background(), first.Token))

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Len(t, h.repo.RevokedFamilies, 1)
	assert.Len(t, h.repo.Created, 1)
}

func TestRotateRevokedStatusWithoutMarkerRevokesTheFamily(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.Tokens[digest(first.Token)].Status = models.RefreshTokenRevoked

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Equal(t, []uuid.UUID{h.familyOf(first.Token)}, h.repo.RevokedFamilies)
	assert.Equal(t, []time.Duration{sessionTTL}, h.repo.RevokeTTLs)
}

func TestRotatePropagatesMarkerLookupErrors(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.FamilyRevokedErrs = []error{errors.New("marker down")}

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "marker down")
	assert.Len(t, h.repo.Created, 1)
}

func TestRotateReportsFamilyRevocationFailures(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.Tokens[digest(first.Token)].Status = models.RefreshTokenRotated
	h.repo.RevokeErr = errors.New("cassandra down")

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "cassandra down")
}

func TestRotateKeepsTheOldTokenWhenTheNewOneCannotBeStored(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.CreateErr = errors.New("cassandra down")

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "cassandra down")
	assert.Equal(t, models.RefreshTokenActive, h.repo.StatusOf(digest(first.Token)))
}

func TestRotatePropagatesCompareAndSetErrors(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.MarkErr = errors.New("ambiguous write")

	_, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "ambiguous write")
	assert.Empty(t, session.Token)
	assert.Empty(t, h.repo.RevokedFamilies)
}

func TestRotateLosingTheCompareAndSetRevokesTheFamilyIncludingTheNewToken(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	firstHash := digest(first.Token)
	h.repo.OnMark = func() {
		if h.repo.Tokens[firstHash].Status == models.RefreshTokenActive {
			h.repo.Tokens[firstHash].Status = models.RefreshTokenRotated
		}
	}

	userID, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Equal(t, uuid.Nil, userID)
	assert.Empty(t, session.Token)
	require.Len(t, h.repo.Created, 2)
	assert.Equal(t, models.RefreshTokenRevoked, h.repo.StatusOf(h.repo.Created[1].TokenHash))
	assert.Equal(t, models.RefreshTokenRotated, h.repo.StatusOf(firstHash))
	assert.Empty(t, h.repo.ActiveIn(h.familyOf(first.Token)))
}

func TestRotateNotAppliedReportsFamilyRevocationFailures(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	applied := false
	h.repo.MarkApplied = &applied
	h.repo.RevokeErr = errors.New("cassandra down")

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "cassandra down")
}

func TestRevokeRevokesEveryTokenInTheFamily(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	_, next, err := h.service.Rotate(context.Background(), first.Token)
	require.NoError(t, err)
	other := h.start(t, uuid.New())

	require.NoError(t, h.service.Revoke(context.Background(), next.Token))

	assert.Equal(t, []uuid.UUID{h.repo.Created[0].FamilyID}, h.repo.RevokedFamilies)
	assert.Equal(t, []time.Duration{sessionTTL}, h.repo.RevokeTTLs)
	assert.True(t, h.repo.Markers[h.repo.Created[0].FamilyID])
	assert.Equal(t, models.RefreshTokenRotated, h.repo.StatusOf(digest(first.Token)))
	assert.Equal(t, models.RefreshTokenRevoked, h.repo.StatusOf(digest(next.Token)))
	assert.Equal(t, models.RefreshTokenActive, h.repo.StatusOf(digest(other.Token)))
	assert.False(t, h.repo.Markers[h.familyOf(other.Token)])

	_, _, err = h.service.Rotate(context.Background(), next.Token)
	assert.ErrorIs(t, err, services.ErrInvalidSession)
}

func TestRevokeIsIdempotent(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())

	require.NoError(t, h.service.Revoke(context.Background(), first.Token))
	require.NoError(t, h.service.Revoke(context.Background(), first.Token))

	assert.Equal(t, models.RefreshTokenRevoked, h.repo.StatusOf(digest(first.Token)))
}

func TestRevokeIgnoresUnknownAndMalformedTokens(t *testing.T) {
	h := newSessionHarness()

	for _, token := range []string{"", "not-a-token", base64.RawURLEncoding.EncodeToString(make([]byte, 32))} {
		assert.NoError(t, h.service.Revoke(context.Background(), token))
	}
	assert.Empty(t, h.repo.RevokedFamilies)
}

func TestRevokeStillRevokesAnExpiredToken(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.clock.T = now.Add(2 * sessionTTL)

	require.NoError(t, h.service.Revoke(context.Background(), first.Token))

	assert.Len(t, h.repo.RevokedFamilies, 1)
}

func TestRevokePropagatesRepositoryErrors(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())

	h.repo.GetErr = errors.New("lookup failed")
	assert.EqualError(t, h.service.Revoke(context.Background(), first.Token), "lookup failed")

	h.repo.GetErr = nil
	h.repo.RevokeErr = errors.New("revoke failed")
	assert.EqualError(t, h.service.Revoke(context.Background(), first.Token), "revoke failed")
}

func TestRevocationBetweenInsertAndCompareAndSetWins(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	family := h.familyOf(first.Token)
	h.repo.OnMark = func() {
		h.repo.OnMark = nil
		require.NoError(t, h.repo.RevokeFamily(context.Background(), family, sessionTTL))
	}

	_, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Empty(t, session.Token)
	assert.Len(t, h.repo.Created, 2)
	assert.Empty(t, h.repo.ActiveIn(family))
}

func TestRevocationRightAfterTheCompareAndSetWins(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	family := h.familyOf(first.Token)
	h.repo.AfterMark = func() {
		h.repo.AfterMark = nil
		require.NoError(t, h.repo.RevokeFamily(context.Background(), family, sessionTTL))
	}

	_, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Empty(t, session.Token)
	assert.Equal(t, models.RefreshTokenRotated, h.repo.StatusOf(digest(first.Token)))
	assert.Empty(t, h.repo.ActiveIn(family))
}

func TestRevocationWithAStaleFamilySnapshotStillWins(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	family := h.familyOf(first.Token)
	var snapshot []string
	h.repo.OnCreate = func() {
		h.repo.OnCreate = nil
		h.repo.Markers[family] = true
		snapshot = append([]string{}, h.repo.Families[family][:1]...)
	}

	_, session, err := h.service.Rotate(context.Background(), first.Token)
	h.repo.RevokeHashes(snapshot)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Empty(t, session.Token)
	assert.Equal(t, models.RefreshTokenRotated, h.repo.StatusOf(digest(first.Token)))
	assert.Empty(t, h.repo.ActiveIn(family))
}

func TestRotateReportsMarkerErrorsAfterTheCompareAndSet(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.FamilyRevokedErrs = []error{nil, errors.New("marker down")}

	_, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "marker down")
	assert.Empty(t, session.Token)
}

func TestRotateReportsRevocationFailuresAfterTheCompareAndSet(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	h.repo.AfterMark = func() {
		h.repo.Markers[h.familyOf(first.Token)] = true
		h.repo.RevokeErr = errors.New("revoke failed")
	}

	_, _, err := h.service.Rotate(context.Background(), first.Token)

	assert.EqualError(t, err, "revoke failed")
}

func TestRotateRejectsFamiliesOlderThanTheMaximumAge(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	stored := h.repo.Tokens[digest(first.Token)]
	stored.FamilyCreatedAt = now.Add(-sessionMaxAge)

	_, session, err := h.service.Rotate(context.Background(), first.Token)

	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Empty(t, session.Token)
	assert.Equal(t, []uuid.UUID{stored.FamilyID}, h.repo.RevokedFamilies)
	assert.Len(t, h.repo.Created, 1)
}

func TestRotateFallsBackToCreatedAtForRowsWithoutFamilyStart(t *testing.T) {
	h := newSessionHarness()
	first := h.start(t, uuid.New())
	stored := h.repo.Tokens[digest(first.Token)]
	stored.FamilyCreatedAt = time.Time{}
	stored.CreatedAt = now.Add(-sessionMaxAge + time.Hour)

	_, next, err := h.service.Rotate(context.Background(), first.Token)

	require.NoError(t, err)
	assert.Equal(t, stored.CreatedAt, h.repo.Created[1].FamilyCreatedAt)
	assert.Equal(t, now.Add(time.Hour), next.ExpiresAt)
	assert.Equal(t, time.Hour, h.repo.TTLs[1])

	second := h.start(t, uuid.New())
	old := h.repo.Tokens[digest(second.Token)]
	old.FamilyCreatedAt = time.Time{}
	old.CreatedAt = now.Add(-sessionMaxAge)

	_, _, err = h.service.Rotate(context.Background(), second.Token)
	assert.ErrorIs(t, err, services.ErrInvalidSession)
	assert.Equal(t, []uuid.UUID{old.FamilyID}, h.repo.RevokedFamilies)
}

func TestSessionsNeverOutliveTheFamilyMaximumAge(t *testing.T) {
	repo := tests.NewFakeRefreshTokenRepo()
	clock := &tests.FakeClock{T: now}
	service := services.NewSessionService(repo, clock, contracts.SessionConfig{RefreshTokenTTL: sessionTTL, FamilyMaxAge: 3 * time.Hour})

	first, err := service.Start(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, now.Add(sessionTTL), first.ExpiresAt)

	clock.T = now.Add(90 * time.Minute)
	_, second, err := service.Rotate(context.Background(), first.Token)
	require.NoError(t, err)
	assert.Equal(t, now.Add(3*time.Hour), second.ExpiresAt)
	assert.Equal(t, 90*time.Minute, repo.TTLs[1])

	clock.T = now.Add(3 * time.Hour)
	_, _, err = service.Rotate(context.Background(), second.Token)
	assert.ErrorIs(t, err, services.ErrInvalidSession)
}
