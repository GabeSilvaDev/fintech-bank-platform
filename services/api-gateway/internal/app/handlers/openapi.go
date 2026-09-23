package handlers

import (
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/api"
)

func OpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(api.Spec)
}
