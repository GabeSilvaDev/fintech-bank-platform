package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/fintech-bank-platform/account-service/internal/contracts"
)

type Server struct {
	server *http.Server
}

func NewServer(cfg contracts.ServerConfig, handler http.Handler) *Server {
	return &Server{server: &http.Server{
		Addr:         cfg.Address(),
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}}
}

func (s *Server) Start() error {
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
