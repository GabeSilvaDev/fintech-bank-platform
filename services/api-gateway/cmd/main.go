package main

import (
	"os"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := config.New()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	server := http.NewServer(cfg, logger)

	http.SetupRouter(server.Router(), cfg)

	if err := server.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Server failed")
	}
}
