package middleware

import (
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/go-chi/cors"
)

func CORS(cfg contracts.CORSConfig) func(next http.Handler) http.Handler {
	return cors.New(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   cfg.AllowedMethods,
		AllowedHeaders:   cfg.AllowedHeaders,
		ExposedHeaders:   cfg.ExposedHeaders,
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           cfg.MaxAge,
	}).Handler
}
