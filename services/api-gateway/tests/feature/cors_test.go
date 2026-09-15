package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/stretchr/testify/suite"
)

type CorsTestSuite struct {
	tests.TestCase
}

func TestCorsSuite(t *testing.T) {
	suite.Run(t, new(CorsTestSuite))
}

func (s *CorsTestSuite) TestCorsHeadersArePresent() {
	s.WithHeader("Origin", "http://localhost:3000").
		Get("/health").
		AssertOk().
		AssertHeaderExists("Access-Control-Allow-Origin")
}

func (s *CorsTestSuite) TestPreflightRequestReturnsOk() {
	s.WithHeader("Origin", "http://localhost:3000").
		WithHeader("Access-Control-Request-Method", "GET").
		Options("/health").
		AssertOk()
}

func (s *CorsTestSuite) TestPreflightRequestHasCorsHeaders() {
	s.WithHeader("Origin", "http://localhost:3000").
		WithHeader("Access-Control-Request-Method", "POST").
		Options("/health").
		AssertHeaderExists("Access-Control-Allow-Methods")
}
