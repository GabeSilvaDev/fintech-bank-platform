package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/stretchr/testify/suite"
)

type NotFoundTestSuite struct {
	tests.TestCase
}

func TestNotFoundSuite(t *testing.T) {
	suite.Run(t, new(NotFoundTestSuite))
}

func (s *NotFoundTestSuite) TestNonExistentRouteReturnsNotFound() {
	s.Get("/non-existent-route").
		AssertNotFound()
}

func (s *NotFoundTestSuite) TestRandomPathReturnsNotFound() {
	randomPath := "/" + tests.RandomString(20)

	s.Get(randomPath).
		AssertNotFound()
}

func (s *NotFoundTestSuite) TestDeepNestedPathReturnsNotFound() {
	s.Get("/api/v1/users/123/orders/456/items").
		AssertNotFound()
}
