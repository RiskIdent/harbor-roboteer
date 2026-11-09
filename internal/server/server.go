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
	"github.com/lestrrat-go/jwx/v4/jwt"
)

//go:generate go tool oapi-codegen -config oapi/cfg.yaml ../../api/openapi.yaml

type Server struct {
	router   *http.ServeMux
	server   *http.Server
	listener net.Listener
	log      *slog.Logger
	harbor   harborClient
	jwtOpts  []jwt.ParseOption // key set, issuer and audience to verify caller tokens
}

type harborClient interface {
	CreateRobot(ctx context.Context, name, project, description string) (*models.RobotCreated, error)
	GetRobotByID(ctx context.Context, id int64) (*models.Robot, error)
	DeleteRobot(ctx context.Context, id int64) error

	Ping(ctx context.Context) error
}

type IServer interface {
	Start(ctx context.Context) error
}

func NewServer(ctx context.Context, addr string, log *slog.Logger, appVersion string, hc harborClient, jwtOpts ...jwt.ParseOption) (*Server, error) {
	r := http.NewServeMux()
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return &Server{}, fmt.Errorf("couldn't create the listener with addr %s, error: %w", addr, err)
	}
	s := &Server{
		router: r,
		server: &http.Server{
			Handler: ChainMiddlewares(r,
				TracingMiddleware(untracedPaths...),
				LoggingMiddleware(log),
			),
		},
		listener: l,
		log:      log,
		harbor:   hc,
		jwtOpts:  jwtOpts,
	}
	if err := s.registerHandlers(appVersion); err != nil {
		_ = l.Close()
		return nil, err
	}
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
