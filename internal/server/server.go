package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type Server struct {
	router   *http.ServeMux
	server   *http.Server
	listener net.Listener
	log      *slog.Logger
}

type IServer interface {
	Start(ctx context.Context) error
}

func NewServer(ctx context.Context, port string, log *slog.Logger) *Server {
	r := http.NewServeMux()
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+port)
	if err != nil {
		log.ErrorContext(ctx, "couldn't create the listener", slog.String("port", port), slog.Any("error", err))
	}
	s := &Server{
		router: r,
		server: &http.Server{
			Handler: NewMiddleware(log).LoggingMiddleware(r),
		},
		listener: l,
		log:      log,
	}
	s.registerHandlers()
	return s
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

	if err := s.server.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return <-shutdownErr
}
