package http

import (
	nethttp "net/http"
	"net/url"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/auth"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/identity"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/owners"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	pkgmw "github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

const upstreamCallTimeout = 5 * time.Second

type Dependencies struct {
	Publisher           contracts.Publisher
	Logger              *logger.Logger
	Metrics             *metrics.Metrics
	AccountService      *url.URL
	TransactionService  *url.URL
	PaymentService      *url.URL
	NotificationService *url.URL
	Owners              contracts.AccountOwners
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

	accountOwners := deps.Owners
	if accountOwners == nil {
		accountOwners = owners.NewClient(deps.AccountService, upstreamCallTimeout, cfg.Auth.OwnerCacheTTL)
	}
	guard := handlers.NewAccessGuard(accountOwners)
	self := guard.RequireSelf("user_id")
	ownAccount := guard.RequireAccountOwner("id")
	ownAccountItems := guard.RequireAccountOwner("account_id")

	account := handlers.NewAccountHandler(deps.Publisher, guard)
	transaction := handlers.NewTransactionHandler(deps.Publisher, guard)
	payment := handlers.NewPaymentHandler(deps.Publisher, guard)
	accountReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.AccountService, "account service"))
	transactionReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.TransactionService, "transaction service"))
	transactionByID := nethttp.StripPrefix("/api/v1", handlers.NewGuardedReadProxy(deps.TransactionService, "transaction service", guard, handlers.ReadAccess{
		Owner:              "account_id",
		Counterparty:       "counterparty_id",
		CounterpartyHidden: []string{"description", "idempotency_key"},
	}))
	paymentReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.PaymentService, "payment service"))
	paymentByID := nethttp.StripPrefix("/api/v1", handlers.NewGuardedReadProxy(deps.PaymentService, "payment service", guard, handlers.ReadAccess{Owner: "account_id"}))
	notificationReads := nethttp.StripPrefix("/api/v1", handlers.NewReadProxy(deps.NotificationService, "notification service"))

	identities := identity.NewClient(deps.AccountService, upstreamCallTimeout)
	authentication := handlers.NewAuthHandler(identities, identities, auth.NewIssuer(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL))
	authLimit := middleware.RateLimit(cfg.AuthRateLimit)
	requireAuth := auth.RequireAuth(auth.NewVerifier(cfg.Auth.JWTSecret))

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/openapi.yaml", handlers.OpenAPI)

		r.Group(func(r chi.Router) {
			r.Use(authLimit)
			r.Post("/auth/register", authentication.Register)
			r.Post("/auth/login", authentication.Login)
			r.Post("/auth/refresh", authentication.Refresh)
			r.Post("/auth/logout", authentication.Logout)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Post("/accounts", account.Create)
			r.Patch("/accounts/{id}", account.Update)
			r.Delete("/accounts/{id}", account.Delete)
			r.With(ownAccount).Get("/accounts/{id}", accountReads.ServeHTTP)
			r.With(self).Get("/users/{user_id}/accounts", accountReads.ServeHTTP)
			r.With(ownAccountItems).Get("/accounts/{account_id}/transactions", transactionReads.ServeHTTP)
			r.Post("/transactions", transaction.Create)
			r.Post("/transfers", transaction.Transfer)
			r.Get("/transactions/{id}", transactionByID.ServeHTTP)
			r.With(ownAccountItems).Get("/accounts/{account_id}/payments", paymentReads.ServeHTTP)
			r.Post("/payments", payment.Process)
			r.Get("/payments/{id}", paymentByID.ServeHTTP)
			r.With(self).Get("/users/{user_id}/notifications", notificationReads.ServeHTTP)
		})
	})
}
