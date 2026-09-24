package feature

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type SessionsAPISuite struct {
	tests.TestCase
}

func TestSessionsAPISuite(t *testing.T) {
	suite.Run(t, new(SessionsAPISuite))
}

func (s *SessionsAPISuite) start(userID string) *tests.TestResponse {
	return s.Post("/sessions", `{"user_id":"`+userID+`"}`)
}

func (s *SessionsAPISuite) rotate(token string) *tests.TestResponse {
	return s.Post("/sessions/rotate", `{"refresh_token":"`+token+`"}`)
}

func (s *SessionsAPISuite) revoke(token string) *tests.TestResponse {
	return s.Post("/sessions/revoke", `{"refresh_token":"`+token+`"}`)
}

func data(response *tests.TestResponse) map[string]interface{} {
	return response.Json()["data"].(map[string]interface{})
}

func (s *SessionsAPISuite) TestStartThenRotate() {
	userID := uuid.NewString()
	started := s.start(userID).AssertCreated().AssertSuccess()
	first := data(started)["refresh_token"].(string)
	s.Len(first, 43)
	s.Equal(s.Clock.T.Truncate(time.Millisecond).Add(time.Hour).Format(time.RFC3339Nano), data(started)["expires_at"])

	rotated := s.rotate(first).AssertOk().AssertJsonPath("data.user_id", userID)
	next := data(rotated)["refresh_token"].(string)
	s.NotEqual(first, next)

	s.rotate(next).AssertOk().AssertJsonPath("data.user_id", userID)
}

func (s *SessionsAPISuite) TestReusingARotatedTokenRevokesTheWholeFamily() {
	first := data(s.start(uuid.NewString()).AssertCreated())["refresh_token"].(string)
	next := data(s.rotate(first).AssertOk())["refresh_token"].(string)

	s.rotate(first).AssertUnauthorized().AssertErrorCode("INVALID_SESSION")
	s.rotate(next).AssertUnauthorized().AssertErrorCode("INVALID_SESSION")
}

func (s *SessionsAPISuite) TestRevokeEndsTheSession() {
	first := data(s.start(uuid.NewString()).AssertCreated())["refresh_token"].(string)

	s.revoke(first).AssertNoContent().AssertBodyEmpty()
	s.revoke(first).AssertNoContent()

	s.rotate(first).AssertUnauthorized().AssertErrorCode("INVALID_SESSION")
}

func (s *SessionsAPISuite) TestRevokeAcceptsUnknownTokens() {
	s.revoke("").AssertNoContent()
	s.revoke("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA").AssertNoContent()
}

func (s *SessionsAPISuite) TestRotateRejectsUnknownMalformedAndExpiredTokens() {
	s.rotate("").AssertUnauthorized().AssertErrorCode("INVALID_SESSION")
	s.rotate("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA").AssertUnauthorized().AssertErrorCode("INVALID_SESSION")

	first := data(s.start(uuid.NewString()).AssertCreated())["refresh_token"].(string)
	s.Clock.T = s.Clock.T.Add(time.Hour)
	s.rotate(first).AssertUnauthorized().AssertErrorCode("INVALID_SESSION")
}

func (s *SessionsAPISuite) TestStartValidatesUserID() {
	s.start("not-a-uuid").
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.user_id", "uuid")
	s.Empty(s.Tokens.Created)
}

func (s *SessionsAPISuite) TestOnlyTheHashIsStoredAndNeverExposed() {
	started := s.start(uuid.NewString()).AssertCreated()
	first := data(started)["refresh_token"].(string)

	s.Require().Len(s.Tokens.Created, 1)
	hash := s.Tokens.Created[0].TokenHash
	s.Len(hash, 64)
	s.NotContains(hash, first)
	s.Equal(models.RefreshTokenActive, s.Tokens.StatusOf(hash))
	started.AssertDontSee(hash)
	s.rotate(first).AssertDontSee(hash)
	s.rotate(first).AssertDontSee(hash).AssertDontSee(first)
}

func (s *SessionsAPISuite) TestRejectsTrailingDataAfterBody() {
	s.Post("/sessions", `{"user_id":"`+uuid.NewString()+`"}{}`).AssertBadRequest().AssertErrorCode("INVALID_JSON")
	s.Empty(s.Tokens.Created)
}

func (s *SessionsAPISuite) TestSessionRoutesOnlyAcceptPost() {
	s.Get("/sessions").AssertMethodNotAllowed()
	s.Get("/sessions/rotate").AssertMethodNotAllowed()
	s.Get("/sessions/revoke").AssertMethodNotAllowed()
}
