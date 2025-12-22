package middleware

import (
	"context"
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/google/uuid"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(contracts.RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		w.Header().Set(contracts.RequestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), contracts.RequestIDKey, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(contracts.RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}
