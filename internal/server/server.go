package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/version"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/prometheus/client_golang/prometheus"
)

// readHeaderTimeout limits how long a client may take to send the request
// headers, so that slow clients can't hold connections open forever
const readHeaderTimeout = 10 * time.Second

type Server struct {
	version     string
	router      *http.ServeMux
	server      *http.Server
	listener    net.Listener
	log         *slog.Logger
	robotOps    *prometheus.CounterVec // robot account operations in Harbor, by operation and result
	httpMetrics *httpMetrics
	harbor      harborClient
	config      *config.Config
	earlyExpiry time.Duration
	jwtOpts     []jwt.ParseOption // key set, issuer and audience to verify caller tokens
}

type harborClient interface {
	CreateRobot(ctx context.Context, robot harbor.RobotCreate) (harbor.Robot, error)
	GetRobotByID(ctx context.Context, id int64) (harbor.Robot, error)
	DeleteRobot(ctx context.Context, id int64) error

	Ping(ctx context.Context) error
}

type IServer interface {
	Start(ctx context.Context) error
}

func NewServer(ctx context.Context, addr string, log *slog.Logger, hc harborClient, cfg *config.Config, earlyExpiry time.Duration, jwtOpts ...jwt.ParseOption) (*Server, error) {
	r := http.NewServeMux()
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("couldn't create the listener with addr %s, error: %w", addr, err)
	}
	robotOps := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "harbor_roboteer_robot_operations_total",
		Help: "Robot account operations in Harbor, by operation and result.",
	}, []string{"operation", "result"})
	for _, op := range []string{"create", "delete"} {
		for _, res := range []string{"success", "failure"} {
			robotOps.WithLabelValues(op, res)
		}
	}
	httpMetrics := newHTTPMetrics(unmonitoredPaths...)
	s := &Server{
		version: version.Get(),
		router:  r,
		server: &http.Server{
			ReadHeaderTimeout: readHeaderTimeout,
			Handler: ChainMiddlewares(r,
				TracingMiddleware(unmonitoredPaths...),
				LoggingMiddleware(log),
				httpMetrics.Middleware, // must stay last: reads r.Pattern
			),
		},
		listener:    l,
		log:         log,
		robotOps:    robotOps,
		httpMetrics: httpMetrics,
		harbor:      hc,
		config:      cfg,
		earlyExpiry: earlyExpiry,
		jwtOpts:     jwtOpts,
	}
	s.registerHandlers()
	return s, nil
}

// Collectors returns the server's metrics, for the caller to register.
func (s *Server) Collectors() []prometheus.Collector {
	return append(s.httpMetrics.collectors(), s.robotOps)
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
