package feature

import (
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type IdentitiesAPISuite struct {
	tests.TestCase
}

func TestIdentitiesAPISuite(t *testing.T) {
	suite.Run(t, new(IdentitiesAPISuite))
}

func (s *IdentitiesAPISuite) register(email, password string) *tests.TestResponse {
	return s.Post("/identities", `{"email":"`+email+`","password":"`+password+`"}`)
}

func (s *IdentitiesAPISuite) verify(email, password string) *tests.TestResponse {
	return s.Post("/identities/verify", `{"email":"`+email+`","password":"`+password+`"}`)
}

func (s *IdentitiesAPISuite) TestRegisterThenVerify() {
	created := s.register(" Ana@Example.com ", "correct horse").AssertCreated().AssertSuccess()
	userID := created.Json()["data"].(map[string]interface{})["user_id"].(string)
	_, err := uuid.Parse(userID)
	s.Require().NoError(err)

	stored, ok := s.Identities.Identities["ana@example.com"]
	s.Require().True(ok)
	s.Equal(userID, stored.UserID.String())
	s.NotEqual("correct horse", stored.PasswordHash)

	s.verify("ANA@example.com", "correct horse").AssertOk().AssertJsonPath("data.user_id", userID)
}

func (s *IdentitiesAPISuite) TestRegisterRejectsDuplicateEmail() {
	s.register("ana@example.com", "correct horse").AssertCreated()

	s.register("ANA@EXAMPLE.COM", "other password").
		AssertStatus(409).
		AssertError().
		AssertErrorCode("EMAIL_TAKEN")
}

func (s *IdentitiesAPISuite) TestRegisterValidatesEmail() {
	s.register("not-an-email", "correct horse").
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.email", "email")
	s.Empty(s.Identities.Created)
}

func (s *IdentitiesAPISuite) TestRegisterValidatesPasswordLength() {
	s.register("ana@example.com", "short").
		AssertUnprocessableEntity().
		AssertErrorCode("VALIDATION_ERROR").
		AssertJsonPath("error.details.password", "length")

	s.register("ana@example.com", strings.Repeat("a", 73)).
		AssertUnprocessableEntity().
		AssertJsonPath("error.details.password", "length")
	s.Empty(s.Identities.Created)
}

func (s *IdentitiesAPISuite) TestVerifyRejectsBadCredentialsWithOneError() {
	s.register("ana@example.com", "correct horse").AssertCreated()

	wrong := s.verify("ana@example.com", "wrong horse").AssertUnauthorized().AssertErrorCode("INVALID_CREDENTIALS")
	unknown := s.verify("ghost@example.com", "correct horse").AssertUnauthorized().AssertErrorCode("INVALID_CREDENTIALS")

	s.Equal(wrong.Body(), unknown.Body())
	s.Len(s.Hasher.Comparisons, 2)
}

func (s *IdentitiesAPISuite) TestVerifyRejectsUnusableEmailLikeWrongPassword() {
	s.register("ana@example.com", "correct horse").AssertCreated()
	wrong := s.verify("ana@example.com", "wrong horse").AssertUnauthorized()

	for _, email := range []string{"", "   ", "not-an-email"} {
		rejected := s.verify(email, "correct horse").AssertUnauthorized().AssertErrorCode("INVALID_CREDENTIALS")
		s.Equal(wrong.Body(), rejected.Body())
	}
	s.Len(s.Hasher.Comparisons, 4)
	_, recorded := s.Failures.Row("not-an-email")
	s.False(recorded)
	s.Len(s.Failures.Writes, 1)
}

func (s *IdentitiesAPISuite) TestVerifyRejectsPasswordBeyondSeventyTwoBytes() {
	long := strings.Repeat("a", 73)
	s.Identities.Identities["ana@example.com"] = &models.Identity{Email: "ana@example.com", UserID: uuid.New(), PasswordHash: "hashed:" + long}

	s.verify("ana@example.com", long).AssertUnauthorized().AssertErrorCode("INVALID_CREDENTIALS")
}

func (s *IdentitiesAPISuite) TestRejectsTrailingDataAfterBody() {
	s.Post("/identities", `{"email":"ana@example.com","password":"correct horse"}{}`).AssertBadRequest().AssertErrorCode("INVALID_JSON")
	s.Empty(s.Identities.Created)
}

func (s *IdentitiesAPISuite) TestResponsesNeverExposePasswordOrHash() {
	s.register("ana@example.com", "correct horse").AssertDontSee("correct horse").AssertDontSee("hashed:")
	s.verify("ana@example.com", "correct horse").AssertDontSee("correct horse").AssertDontSee("hashed:")
	s.verify("ana@example.com", "wrong horse").AssertDontSee("wrong horse").AssertDontSee("hashed:")
}

func (s *IdentitiesAPISuite) TestIdentityRoutesOnlyAcceptPost() {
	s.Get("/identities").AssertMethodNotAllowed()
	s.Get("/identities/verify").AssertMethodNotAllowed()
}

func (s *IdentitiesAPISuite) TestVerifyLocksOutAfterRepeatedFailures() {
	s.register("ana@example.com", "correct horse").AssertCreated()
	for i := 0; i < 5; i++ {
		s.verify("ana@example.com", "wrong horse").AssertUnauthorized().AssertErrorCode("INVALID_CREDENTIALS")
	}

	s.verify("ANA@example.com", "correct horse").
		AssertTooManyRequests().
		AssertErrorCode("TOO_MANY_ATTEMPTS").
		AssertJsonPath("error.message", "too many failed attempts; try again later").
		AssertHeader("Retry-After", "900")

	s.Clock.T = s.Clock.T.Add(14*time.Minute + 30*time.Second)
	s.verify("ana@example.com", "correct horse").AssertTooManyRequests().AssertHeader("Retry-After", "30")

	s.Clock.T = s.Clock.T.Add(30 * time.Second)
	s.verify("ana@example.com", "correct horse").AssertOk()
	_, recorded := s.Failures.Row("ana@example.com")
	s.False(recorded)
}

func (s *IdentitiesAPISuite) TestVerifyLocksOutUnknownEmailsLikeKnownOnes() {
	s.register("ana@example.com", "correct horse").AssertCreated()
	for i := 0; i < 5; i++ {
		s.verify("ana@example.com", "wrong horse").AssertUnauthorized()
		s.verify("ghost@example.com", "wrong horse").AssertUnauthorized()
	}

	known := s.verify("ana@example.com", "correct horse").AssertTooManyRequests().AssertHeader("Retry-After", "900")
	unknown := s.verify("ghost@example.com", "correct horse").AssertTooManyRequests().AssertHeader("Retry-After", "900")

	s.Equal(known.Body(), unknown.Body())
}

func (s *IdentitiesAPISuite) TestBusyHashingAsksClientsToRetryAfterOneSecond() {
	entered := make(chan struct{})
	release := make(chan struct{})
	s.Verifier.WithHashConcurrency(1, 10*time.Millisecond)
	s.Hasher.OnHash = func() {
		entered <- struct{}{}
		<-release
	}
	done := make(chan *tests.TestResponse, 1)
	go func() { done <- s.register("first@example.com", "correct horse") }()
	<-entered

	s.verify("ana@example.com", "correct horse").
		AssertStatus(503).
		AssertErrorCode("SERVICE_BUSY").
		AssertHeader("Retry-After", "1")
	s.register("second@example.com", "correct horse").AssertStatus(503).AssertHeader("Retry-After", "1")
	released, _ := s.Failures.Row("ana@example.com")
	s.Equal(0, released.Failures)

	close(release)
	(<-done).AssertCreated()
}
