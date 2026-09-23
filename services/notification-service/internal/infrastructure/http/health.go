package http

import (
	"context"
	"net/http"

	"github.com/fintech-bank-platform/pkg/response"
)

func healthHandler(ping func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := ping(r.Context()); err != nil {
			response.JSON(w, http.StatusServiceUnavailable, response.Response{Success: false, Data: map[string]string{"status": "degraded", "redis": "down"}})
			return
		}
		response.OK(w, map[string]string{"status": "healthy", "redis": "up"})
	}
}
