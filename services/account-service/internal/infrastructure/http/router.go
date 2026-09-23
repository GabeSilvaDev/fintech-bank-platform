package http

import (
	"context"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Reads  *handlers.ReadHandler
	Ping   func(context.Context) error
	Logger *logger.Logger
}

func SetupRouter(router *chi.Mux, deps Dependencies) {
	router.Use(middleware.RequestID)
	router.Use(chiMiddleware.RealIP)
	router.Use(middleware.Logger(deps.Logger))
	router.Use(middleware.Recovery)
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler(deps.Ping))
	router.Get("/accounts/{id}", deps.Reads.GetAccount)
	router.Get("/accounts/{id}/owner", deps.Reads.GetOwner)
	router.Get("/users/{user_id}/accounts", deps.Reads.ListUserAccounts)
}
