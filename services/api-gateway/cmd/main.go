package main

import (
	"context"
	"net/url"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	gwmsg "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/messaging"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/tracing"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		logger.NewDefault().Fatal().Err(err).Msg("Failed to load configuration")
	}

	log := logger.New(logger.Config{Level: cfg.Log.Level, Pretty: cfg.Log.Pretty})
	if cfg.Auth.DevelopmentSecret {
		log.Warn().Msg("JWT_SECRET is the published development value; set a private secret before exposing the gateway")
	}

	shutdownTracing, err := tracing.Init(context.Background(), tracing.Config{
		Service:     "api-gateway",
		Endpoint:    cfg.Observability.OTLPEndpoint,
		SampleRatio: cfg.Observability.SampleRatio,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize tracing")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown tracing")
		}
	}()

	var m *metrics.Metrics
	if cfg.Observability.MetricsEnabled {
		m = metrics.New("api-gateway")
	}

	upstream, err := url.Parse(cfg.Upstreams.AccountService)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid ACCOUNT_SERVICE_URL")
	}

	transactionService, err := url.Parse(cfg.Upstreams.TransactionService)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid TRANSACTION_SERVICE_URL")
	}

	paymentService, err := url.Parse(cfg.Upstreams.PaymentService)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid PAYMENT_SERVICE_URL")
	}

	notificationService, err := url.Parse(cfg.Upstreams.NotificationService)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid NOTIFICATION_SERVICE_URL")
	}

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	}).WithMetrics(m)

	server := http.NewServer(cfg, log.Logger)
	http.SetupRouter(server.Router(), cfg, http.Dependencies{
		Publisher:           gwmsg.NewBreaker(producer, cfg.Kafka, m),
		Logger:              log,
		Metrics:             m,
		AccountService:      upstream,
		TransactionService:  transactionService,
		PaymentService:      paymentService,
		NotificationService: notificationService,
	})

	err = server.Start()
	producer.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}
