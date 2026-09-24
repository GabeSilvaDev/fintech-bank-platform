package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/stretchr/testify/suite"
)

type NotificationsTestSuite struct {
	tests.TestCase
}

func TestNotificationsSuite(t *testing.T) {
	suite.Run(t, new(NotificationsTestSuite))
}

func (s *NotificationsTestSuite) TestReadRoutesAreProxied() {
	s.Get("/api/v1/users/" + s.UserID.String() + "/notifications").AssertStatus(502).AssertErrorCode("UPSTREAM_UNAVAILABLE")
}
