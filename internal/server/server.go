package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

type Server struct {
	router   *http.ServeMux
	server   *http.Server
	listener net.Listener
	log      *slog.Logger
	harbor   harborClient
}

type harborClient interface {
	CreateRobot(ctx context.Context, name, project, description string) (*models.RobotCreated, error)
	GetRobotByID(ctx context.Context, id int64) (*models.Robot, error)
	DeleteRobot(ctx context.Context, id int64) error
}

type IServer interface {
	Start(ctx context.Context) error
}

func NewServer(ctx context.Context, addr string, log *slog.Logger, hc harborClient) (*Server, error) {
	r := http.NewServeMux()
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return &Server{}, fmt.Errorf("couldn't create the listener with addr %s, error: %w", addr, err)
	}
	s := &Server{
		router: r,
		server: &http.Server{
			Handler: NewMiddleware(log).LoggingMiddleware(r),
		},
		listener: l,
		log:      log,
		harbor:   hc,
	}
	s.registerHandlers()
	return s, nil
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
