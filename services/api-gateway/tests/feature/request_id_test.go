// ═══════════════════════════════════════════════════════════════════════════
// Feature Test: Request ID Middleware
// ═══════════════════════════════════════════════════════════════════════════

package feature

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/stretchr/testify/suite"
)

// ═══════════════════════════════════════════════════════════════════════════
// Test Suite
// ═══════════════════════════════════════════════════════════════════════════

type RequestIDTestSuite struct {
	tests.TestCase
}

func TestRequestIDSuite(t *testing.T) {
	suite.Run(t, new(RequestIDTestSuite))
}

// ═══════════════════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════════════════

func (s *RequestIDTestSuite) TestRequestIdIsGenerated() {
	s.Get("/health").
		AssertOk().
		AssertHeaderExists("X-Request-ID")
}

func (s *RequestIDTestSuite) TestProvidedRequestIdIsPreserved() {
	customRequestID := "custom-request-id-12345"

	s.WithHeader("X-Request-ID", customRequestID).
		Get("/health").
		AssertOk().
		AssertHeader("X-Request-ID", customRequestID)
}

func (s *RequestIDTestSuite) TestGeneratedRequestIdIsUUID() {
	response := s.Get("/health")
	response.AssertOk()

	requestID := response.Header("X-Request-ID")
	s.NotEmpty(requestID)
	s.Regexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`, requestID)
}
