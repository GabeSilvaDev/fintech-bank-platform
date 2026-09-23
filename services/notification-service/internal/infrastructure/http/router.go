package http

import (
	"context"

	"github.com/fintech-bank-platform/notification-service/internal/app/handlers"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	History *handlers.HistoryHandler
	Ping    func(context.Context) error
	Logger  *logger.Logger
}

func SetupRouter(router *chi.Mux, deps Dependencies) {
	router.Use(middleware.RequestID)
	router.Use(chiMiddleware.RealIP)
	router.Use(middleware.Logger(deps.Logger))
	router.Use(middleware.Recovery)
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler(deps.Ping))
	router.Get("/users/{user_id}/notifications", deps.History.ListUserNotifications)
}
