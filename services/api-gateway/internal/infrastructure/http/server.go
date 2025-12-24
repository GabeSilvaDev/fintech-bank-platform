package http

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

type Server struct {
	server *http.Server
	router *chi.Mux
	config *config.Config
	logger zerolog.Logger
}

func NewServer(cfg *config.Config, logger zerolog.Logger) *Server {
	router := chi.NewRouter()

	srv := &Server{
		router: router,
		config: cfg,
		logger: logger,
	}

	srv.server = &http.Server{
		Addr:         cfg.Server.Address(),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	return srv
}

func (s *Server) Router() *chi.Mux {
	return s.router
}

func (s *Server) Start() error {
	return s.StartWithSignals(syscall.SIGINT, syscall.SIGTERM)
}

func (s *Server) StartWithSignals(signals ...os.Signal) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, signals...)

	serverErrors := make(chan error, 1)

	go func() {
		s.logger.Info().Str("address", s.config.Server.Address()).Msg("Server starting")

		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		return err
	case <-quit:
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return err
	}

	s.logger.Info().Msg("Server stopped")
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) Address() string {
	return s.config.Server.Address()
}
