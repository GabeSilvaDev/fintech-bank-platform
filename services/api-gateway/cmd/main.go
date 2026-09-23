package main

import (
	"net/url"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	gwmsg "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/messaging"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/messaging"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		logger.NewDefault().Fatal().Err(err).Msg("Failed to load configuration")
	}

	log := logger.New(logger.Config{Level: cfg.Log.Level, Pretty: cfg.Log.Pretty})

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

	producer := messaging.NewProducer(messaging.ProducerConfig{
		Brokers:        cfg.Kafka.Brokers,
		WriteTimeout:   cfg.Kafka.WriteTimeout,
		BatchTimeout:   cfg.Kafka.BatchTimeout,
		PublishTimeout: cfg.Kafka.PublishTimeout,
		MaxAttempts:    cfg.Kafka.MaxAttempts,
	})

	server := http.NewServer(cfg, log.Logger)
	http.SetupRouter(server.Router(), cfg, http.Dependencies{
		Publisher:          gwmsg.NewBreaker(producer, cfg.Kafka),
		Logger:             log,
		AccountService:     upstream,
		TransactionService: transactionService,
		PaymentService:     paymentService,
	})

	err = server.Start()
	producer.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}
