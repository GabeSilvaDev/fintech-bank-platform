// ═══════════════════════════════════════════════════════════════════════════
// Feature Test: Health Endpoint
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

type HealthTestSuite struct {
	tests.TestCase
}

func TestHealthSuite(t *testing.T) {
	suite.Run(t, new(HealthTestSuite))
}

// ═══════════════════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════════════════

func (s *HealthTestSuite) TestHealthEndpointReturnsOk() {
	s.Get("/health").
		AssertOk().
		AssertSuccess().
		AssertJsonHas("data.status").
		AssertJsonPath("data.status", "healthy")
}

func (s *HealthTestSuite) TestHealthEndpointReturnsJsonContentType() {
	s.Get("/health").
		AssertOk().
		AssertContentType("application/json")
}

func (s *HealthTestSuite) TestHealthEndpointHasRequiredFields() {
	s.Get("/health").
		AssertOk().
		AssertJsonStructure([]string{
			"success",
			"data",
		}).
		AssertJsonHas("data.status").
		AssertJsonHas("data.timestamp").
		AssertJsonHas("data.uptime")
}

func (s *HealthTestSuite) TestHealthEndpointWithCustomHeader() {
	s.WithHeader("X-Custom-Header", "test-value").
		Get("/health").
		AssertOk().
		AssertSuccess()
}

func (s *HealthTestSuite) TestHealthEndpointMethodNotAllowed() {
	s.Post("/health", nil).
		AssertMethodNotAllowed()
}
