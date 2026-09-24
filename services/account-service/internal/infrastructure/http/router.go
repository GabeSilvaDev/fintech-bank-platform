package http

import (
	"context"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Reads      *handlers.ReadHandler
	Identities *handlers.IdentityHandler
	Sessions   *handlers.SessionHandler
	Ping       func(context.Context) error
	Logger     *logger.Logger
	Metrics    *metrics.Metrics
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

	router.Get("/accounts/{id}", deps.Reads.GetAccount)
	router.Get("/accounts/{id}/owner", deps.Reads.GetOwner)
	router.Get("/users/{user_id}/accounts", deps.Reads.ListUserAccounts)
	router.Post("/identities", deps.Identities.Register)
	router.Post("/identities/verify", deps.Identities.Verify)
	router.Post("/sessions", deps.Sessions.Start)
	router.Post("/sessions/rotate", deps.Sessions.Rotate)
	router.Post("/sessions/revoke", deps.Sessions.Revoke)
}
