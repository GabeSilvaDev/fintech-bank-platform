package http

import (
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

func SetupRouter(router *chi.Mux, cfg *config.Config) {
	router.Use(middleware.RequestID)
	router.Use(middleware.Recovery)
	router.Use(chiMiddleware.RealIP)
	router.Use(middleware.CORS(cfg.CORS))
	router.Use(middleware.RateLimit(cfg.RateLimit))
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler)
}
