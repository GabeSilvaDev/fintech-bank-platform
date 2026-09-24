package auth

import (
	"context"
	"net/http"
	"strings"

	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/google/uuid"
)

type userIDKey struct{}

func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

func UserID(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return userID, ok
}

func RequireAuth(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := verifier.Verify(bearerToken(r))
			if err != nil {
				w.Header().Set("WWW-Authenticate", TokenType)
				response.FromError(w, apperrors.Unauthorized("UNAUTHORIZED", "authentication required"))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
		})
	}
}

func bearerToken(r *http.Request) string {
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	if !strings.EqualFold(scheme, TokenType) {
		return ""
	}
	return strings.TrimSpace(token)
}
