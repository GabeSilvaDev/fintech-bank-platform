package http

import (
	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Publisher contracts.Publisher
	Logger    *logger.Logger
}

func SetupRouter(router *chi.Mux, cfg *config.Config, deps Dependencies) {
	router.Use(middleware.RequestID)
	router.Use(chiMiddleware.RealIP)
	router.Use(middleware.Logger(deps.Logger))
	router.Use(middleware.Recovery)
	router.Use(middleware.CORS(cfg.CORS))
	router.Use(middleware.RateLimit(cfg.RateLimit))
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler)

	account := handlers.NewAccountHandler(deps.Publisher)
	transaction := handlers.NewTransactionHandler(deps.Publisher)
	payment := handlers.NewPaymentHandler(deps.Publisher)

	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/accounts", account.Create)
		r.Patch("/accounts/{id}", account.Update)
		r.Delete("/accounts/{id}", account.Delete)
		r.Post("/transactions", transaction.Create)
		r.Post("/transfers", transaction.Transfer)
		r.Post("/payments", payment.Process)
	})
}
