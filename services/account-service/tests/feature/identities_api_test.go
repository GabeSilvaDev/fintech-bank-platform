package feature

import (
	"strings"
	"testing"

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
