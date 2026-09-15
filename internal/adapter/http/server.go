package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/config"
)

// Server is the HTTP adapter. Domain code must not import this package.
type Server struct {
	addr         string
	log          *slog.Logger
	srv          *http.Server
	checkers     Checkers
	shuttingDown atomic.Bool
}

// New constructs the HTTP server and registers public health routes.
func New(cfg config.Config, log *slog.Logger, checkers Checkers) *Server {
	s := &Server{
		addr:     cfg.HTTPAddr,
		log:      log,
		checkers: checkers,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.handleLive)
	mux.HandleFunc("GET /health/ready", s.handleReady)
	s.srv = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Handler exposes the mux for tests.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

// Start binds the listen address synchronously so a bind error fails Fx start,
// then serves in a goroutine.
func (s *Server) Start(_ context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("http: listen %s: %w", s.addr, err)
	}
	s.log.Info("http listening", "addr", ln.Addr().String())
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http server", "err", err)
		}
	}()
	return nil
}

// Stop marks readiness as failing immediately, then shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.shuttingDown.Store(true)
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("http: shutdown: %w", err)
	}
	return nil
}
