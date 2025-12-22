package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
)

var startTime = time.Now()

func healthHandler(w http.ResponseWriter, r *http.Request) {
	response := contracts.Response{
		Success: true,
		Data: map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"uptime":    time.Since(startTime).String(),
		},
	}
	jsonResponse(w, http.StatusOK, response)
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
