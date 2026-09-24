package http

import (
	nethttp "net/http"
	"net/url"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	pkgmw "github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Publisher           contracts.Publisher
	Logger              *logger.Logger
	Metrics             *metrics.Metrics
	AccountService      *url.URL
	TransactionService  *url.URL
	PaymentService      *url.URL
	NotificationService *url.URL
}

func SetupRouter(router *chi.Mux, cfg *config.Config, deps Dependencies) {
	router.Use(pkgmw.RequestID)
	router.Use(tracing.EdgeMiddleware)
	router.Use(deps.Metrics.Middleware)
	if cfg.Server.TrustProxyHeaders {
		hops := cfg.Server.TrustedProxyHops
		if hops < 1 {
			hops = 1
		}
		router.Use(chiMiddleware.ClientIPFromXFFTrustedProxies(hops))
	} else {
		router.Use(chiMiddleware.ClientIPFromRemoteAddr)
	}
	router.Use(pkgmw.Logger(deps.Logger))
	router.Use(pkgmw.Recovery(deps.Logger))
	router.Use(middleware.CORS(cfg.CORS))
	router.Use(middleware.RateLimit(cfg.RateLimit))
	router.Use(chiMiddleware.StripSlashes)

	router.Get("/health", healthHandler)

	if deps.Metrics != nil {
		router.Get("/metrics", deps.Metrics.Handler().ServeHTTP)
	}

	account := handlers.NewAccountHandler(deps.Publisher)
	transaction := handlers.NewTransactionHandler(deps.Publisher)
	payment := handlers.NewPaymentHandler(deps.Publisher)
	accountReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.AccountService, "account service"))
	transactionReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.TransactionService, "transaction service"))
	paymentReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.PaymentService, "payment service"))
	notificationReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.NotificationService, "notification service"))

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/openapi.yaml", handlers.OpenAPI)
		r.Post("/accounts", account.Create)
		r.Patch("/accounts/{id}", account.Update)
		r.Delete("/accounts/{id}", account.Delete)
		r.Get("/accounts/{id}", accountReads.ServeHTTP)
		r.Get("/users/{user_id}/accounts", accountReads.ServeHTTP)
		r.Get("/accounts/{account_id}/transactions", transactionReads.ServeHTTP)
		r.Post("/transactions", transaction.Create)
		r.Post("/transfers", transaction.Transfer)
		r.Get("/transactions/{id}", transactionReads.ServeHTTP)
		r.Get("/accounts/{account_id}/payments", paymentReads.ServeHTTP)
		r.Post("/payments", payment.Process)
		r.Get("/payments/{id}", paymentReads.ServeHTTP)
		r.Get("/users/{user_id}/notifications", notificationReads.ServeHTTP)
	})
}
