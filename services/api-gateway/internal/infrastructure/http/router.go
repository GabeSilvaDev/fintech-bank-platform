package http

import (
	nethttp "net/http"
	"net/url"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/fintech-bank-platform/pkg/logger"
	pkgmw "github.com/fintech-bank-platform/pkg/middleware"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Publisher          contracts.Publisher
	Logger             *logger.Logger
	AccountService     *url.URL
	TransactionService *url.URL
}

func SetupRouter(router *chi.Mux, cfg *config.Config, deps Dependencies) {
	router.Use(pkgmw.RequestID)
	router.Use(chiMiddleware.RealIP)
	router.Use(pkgmw.Logger(deps.Logger))
	router.Use(pkgmw.Recovery)
	router.Use(middleware.CORS(cfg.CORS))
	router.Use(middleware.RateLimit(cfg.RateLimit))
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler)

	account := handlers.NewAccountHandler(deps.Publisher)
	transaction := handlers.NewTransactionHandler(deps.Publisher)
	payment := handlers.NewPaymentHandler(deps.Publisher)
	accountReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.AccountService))
	transactionReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.TransactionService))

	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/accounts", account.Create)
		r.Patch("/accounts/{id}", account.Update)
		r.Delete("/accounts/{id}", account.Delete)
		r.Get("/accounts/{id}", accountReads.ServeHTTP)
		r.Get("/users/{user_id}/accounts", accountReads.ServeHTTP)
		r.Get("/accounts/{account_id}/transactions", transactionReads.ServeHTTP)
		r.Post("/transactions", transaction.Create)
		r.Post("/transfers", transaction.Transfer)
		r.Get("/transactions/{id}", transactionReads.ServeHTTP)
		r.Post("/payments", payment.Process)
	})
}
