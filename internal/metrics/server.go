package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const readHeaderTimeout = 10 * time.Second

type Server struct {
	server   *http.Server
	listener net.Listener
	log      *slog.Logger
}

type MetricsServer interface {
	Start(ctx context.Context) error
}

func NewServer(ctx context.Context, log *slog.Logger, addr string, reg *Registry) (*Server, error) {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("metrics listener %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog: slog.NewLogLogger(log.Handler().WithGroup("metrics"), slog.LevelError),
	}))

	return &Server{
		server: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: readHeaderTimeout,
		},
		listener: l,
		log:      log,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	shutdownErr := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		shutdownErr <- s.server.Shutdown(shutdownCtx)
	}()

	s.log.InfoContext(ctx, "starting server", slog.String("addr", s.listener.Addr().String()))
	if err := s.server.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return <-shutdownErr
}
