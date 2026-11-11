package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/metrics"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// readHeaderTimeout limits how long a client may take to send the request
// headers, so that slow clients can't hold connections open forever
const readHeaderTimeout = 10 * time.Second

type Server struct {
	router   *http.ServeMux
	server   *http.Server
	listener net.Listener
	metrics  metrics.Middleware
	robotOps *prometheus.CounterVec
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

func NewServer(ctx context.Context, addr string, reg *metrics.Registry, log *slog.Logger, hc harborClient, jwtOpts ...jwt.ParseOption) (*Server, error) {
	r := http.NewServeMux()
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("couldn't create the listener with addr %s, error: %w", addr, err)
	}
	robotOps := promauto.With(reg.Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "harbor_roboteer_robot_operations_total",
		Help: "Robot account operations in Harbor, by operation and result.",
	}, []string{"operation", "result"})
	for _, op := range []string{"create", "delete"} {
		for _, res := range []string{"success", "failure"} {
			robotOps.WithLabelValues(op, res)
		}
	}
	s := &Server{
		router: r,
		server: &http.Server{
			ReadHeaderTimeout: readHeaderTimeout,
			Handler: ChainMiddlewares(r,
				TracingMiddleware(untracedPaths...),
				LoggingMiddleware(log),
			),
		},
		listener: l,
		metrics:  metrics.NewMiddleware(reg.Registry, nil),
		robotOps: robotOps,
		log:      log,
		harbor:   hc,
		jwtOpts:  jwtOpts,
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
