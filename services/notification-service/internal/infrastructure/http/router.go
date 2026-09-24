package http

import (
	"context"

	"github.com/fintech-bank-platform/notification-service/internal/app/handlers"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	History *handlers.HistoryHandler
	Ping    func(context.Context) error
	Logger  *logger.Logger
	Metrics *metrics.Metrics
}

func SetupRouter(router *chi.Mux, deps Dependencies) {
	router.Use(middleware.RequestID)
	router.Use(tracing.Middleware)
	router.Use(deps.Metrics.Middleware)
	router.Use(middleware.Logger(deps.Logger))
	router.Use(middleware.Recovery(deps.Logger))
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler(deps.Ping))

	if deps.Metrics != nil {
		router.Get("/metrics", deps.Metrics.Handler().ServeHTTP)
	}

	router.Get("/users/{user_id}/notifications", deps.History.ListUserNotifications)
}
