package tests

import (
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/auth"
	"github.com/google/uuid"
)

const JWTSecret = "test-only-jwt-secret-0123456789abcdef"

func AuthConfig() contracts.AuthConfig {
	return contracts.AuthConfig{
		JWTSecret:             JWTSecret,
		TokenTTL:              time.Hour,
		OwnerCacheTTL:         time.Minute,
		OwnerNegativeCacheTTL: 5 * time.Second,
	}
}

func AccessToken(userID uuid.UUID) string {
	token, _, _ := auth.NewIssuer(JWTSecret, time.Hour).Issue(userID)
	return token
}

func BearerToken(userID uuid.UUID) string {
	return "Bearer " + AccessToken(userID)
}
