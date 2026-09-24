package unit

import (
	"os"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/tests"
)

func TestMain(m *testing.M) {
	os.Setenv("JWT_SECRET", tests.JWTSecret)
	os.Exit(m.Run())
}
