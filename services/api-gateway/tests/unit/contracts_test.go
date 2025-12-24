// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: Contracts
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/stretchr/testify/assert"
)

func TestServerConfigAddress(t *testing.T) {
	cfg := contracts.ServerConfig{
		Host: "localhost",
		Port: "8080",
	}

	assert.Equal(t, "localhost:8080", cfg.Address())
}

func TestServerConfigAddressWithDifferentValues(t *testing.T) {
	cfg := contracts.ServerConfig{
		Host: "0.0.0.0",
		Port: "3000",
	}

	assert.Equal(t, "0.0.0.0:3000", cfg.Address())
}
