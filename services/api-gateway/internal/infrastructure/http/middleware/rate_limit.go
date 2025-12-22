package middleware

import (
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/go-chi/httprate"
)

func RateLimit(cfg contracts.RateLimitConfig) func(next http.Handler) http.Handler {
	return httprate.Limit(
		cfg.Requests,
		cfg.Window,
		httprate.WithKeyFuncs(httprate.KeyByIP),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"success":false,"error":{"code":"RATE_LIMIT_EXCEEDED","message":"Too many requests"}}`))
		}),
	)
}
