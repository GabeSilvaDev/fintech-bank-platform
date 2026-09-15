package main

import (
	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/messaging"
	"github.com/fintech-bank-platform/pkg/logger"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		logger.NewDefault().Fatal().Err(err).Msg("Failed to load configuration")
	}

	log := logger.New(logger.Config{Level: cfg.Log.Level, Pretty: cfg.Log.Pretty})

	producer := messaging.NewProducer(cfg.Kafka)

	server := http.NewServer(cfg, log.Logger)
	http.SetupRouter(server.Router(), cfg, http.Dependencies{
		Publisher: messaging.NewBreaker(producer, cfg.Kafka),
		Logger:    log,
	})

	err = server.Start()
	producer.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}
