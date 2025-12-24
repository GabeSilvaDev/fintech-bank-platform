package http

import (
	"net/http"
	"time"

	"github.com/fintech-bank-platform/pkg/response"
)

var startTime = time.Now()

func healthHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    time.Since(startTime).String(),
	}
	response.OK(w, data)
}
