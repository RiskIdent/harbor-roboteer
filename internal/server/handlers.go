package server

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

const deleteTokenPrefix = "sha256:"

// maxBodyBytes is the largest request body that POST /token accepts
const maxBodyBytes = 64 << 10

// errInvalidDeleteCombo is the same for an unknown robot and a wrong delete
// token, so that callers can't use it to find out which robot IDs exist
const errInvalidDeleteCombo = "invalid robot ID and delete token combination"

type postRobotRequest struct {
	Name string `json:"name"`
}

type robotCredentials struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type postRobotResponse struct {
	Robot       robotCredentials `json:"robot"`
	DeleteToken string           `json:"deleteToken"`
}

var (
	unmonitoredPaths = []string{
		"/api/v1/health",
	}

	errNoPermissions  = errors.New("config grants no permissions to token")
	errEvaluateConfig = errors.New("evaluate permissions")

	now = time.Now // overridden in tests
)

func (s *Server) registerHandlers() {
	s.registerV1()
}

func (s *Server) registerV1() {
	r := s.router

	const prefix = "/api/v1"
	r.HandleFunc("POST "+prefix+"/robot", s.postRobot)
	r.HandleFunc("DELETE "+prefix+"/robot", s.deleteRobot)

	r.HandleFunc("GET "+prefix+"/health", s.health)
}

func (s *Server) postRobot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok, err := jwt.ParseRequest(r, s.JWTOptions...)
	if err != nil {
		s.Log.ErrorContext(ctx, "reject token", slog.Any("error", err))
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "body must be valid JSON", http.StatusBadRequest)
		return
	}
	var req postRobotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "body must be valid JSON", http.StatusBadRequest)
		return
	}
	sub, _ := tok.Subject()
	name := cmp.Or(req.Name, sub)
	if name == "" {
		s.Log.ErrorContext(ctx, "token has no sub claim and no name was provided in the request body")
		http.Error(w, "token has no sub claim, set name in the request body", http.StatusBadRequest)
		return
	}

	deleteToken := rand.Text()
	robot, permissions, err := buildRobotCreate(robotCreateOptions{
		Config:      s.Config,
		Token:       tok,
		Subject:     sub,
		Name:        name,
		DeleteToken: deleteToken,
		Version:     s.version,
		RobotTTL:    s.RobotTTL,
	})
	if errors.Is(err, errNoPermissions) {
		s.Log.ErrorContext(ctx, "reject token", slog.String("sub", sub), slog.Any("error", err))
		http.Error(w, "token is not granted any permissions", http.StatusForbidden)
		return
	}
	if err != nil {
		s.Log.ErrorContext(ctx, "build robot create request", slog.Any("error", err))
		http.Error(w, "failed to evaluate permissions from config", http.StatusInternalServerError)
		return
	}
	created, err := s.Harbor.CreateRobot(ctx, robot)
	s.countRobotOp("create", err)
	if errors.Is(err, harbor.ErrRobotExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		s.Log.ErrorContext(ctx, "create robot account", slog.Any("error", err))
		http.Error(w, "failed to create robot account in Harbor", http.StatusBadGateway)
		return
	}
	s.Log.InfoContext(ctx, "created robot account",
		slog.String("sub", sub),
		slog.Int64("robotId", created.ID),
		slog.String("robotName", created.Name),
		slog.Any("permissions", permissions),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(postRobotResponse{
		Robot: robotCredentials{
			ID:       created.ID,
			Username: created.Name,
			Password: created.Secret,
		},
		DeleteToken: deleteToken,
	})
}

type robotCreateOptions struct {
	Config      *config.Config
	Token       jwt.Token
	Subject     string
	Name        string
	DeleteToken string
	Version     string
	RobotTTL    time.Duration
}

func buildRobotCreate(options robotCreateOptions) (harbor.RobotCreate, config.Permissions, error) {
	permissions, err := options.Config.Evaluate(maps.Collect(options.Token.Claims()))
	if err != nil {
		return harbor.RobotCreate{}, config.Permissions{}, fmt.Errorf("%w: %w", errEvaluateConfig, err)
	}
	if permissions.IsEmpty() {
		return harbor.RobotCreate{}, config.Permissions{}, errNoPermissions
	}

	hash := sha256.Sum256([]byte(options.DeleteToken))
	return harbor.RobotCreate{
		NameBase:              options.Name,
		ProjectPermissions:    harborProjectPermissions(permissions.Projects),
		AllProjectPermissions: harborPermissions(permissions.AllProjects),
		SystemPermissions:     harborPermissions(permissions.System),
		Expiry:                now().Add(options.RobotTTL),
		Description:           fmt.Sprintf("Robot account for OIDC user %q", cmp.Or(options.Subject, "(unknown JWT sub)")),
		DeleteToken:           deleteTokenPrefix + hex.EncodeToString(hash[:]),
		ManagedBy:             "harbor-roboteer " + options.Version,
	}, permissions, nil
}

func harborProjectPermissions(permissions map[string]config.Access) map[string]harbor.Permissions {
	if permissions == nil {
		return nil
	}
	result := make(map[string]harbor.Permissions, len(permissions))
	for project, projectPermissions := range permissions {
		result[project] = harborPermissions(projectPermissions)
	}
	return result
}

func harborPermissions(access config.Access) harbor.Permissions {
	if access == nil {
		return nil
	}
	result := make(harbor.Permissions, len(access))
	for resource, actions := range access {
		result[resource] = actions
	}
	return result
}

func (s *Server) deleteRobot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token, ok := bearerToken(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Authorization: Bearer <delete token> header is required", http.StatusUnauthorized)
		return
	}

	robotID, err := strconv.ParseInt(r.URL.Query().Get("robot_id"), 10, 64)
	if err != nil {
		http.Error(w, "robot_id query parameter must be an integer", http.StatusBadRequest)
		return
	}

	rb, err := s.Harbor.GetRobotByID(ctx, robotID)
	if errors.Is(err, harbor.ErrRobotNotFound) {
		http.Error(w, errInvalidDeleteCombo, http.StatusForbidden)
		return
	}
	if err != nil {
		s.Log.ErrorContext(ctx, "get robot account", slog.Any("error", err))
		http.Error(w, "failed to get robot account from Harbor", http.StatusBadGateway)
		return
	}

	hash := sha256.Sum256([]byte(token))
	want := deleteTokenPrefix + hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(rb.DeleteToken), []byte(want)) != 1 {
		http.Error(w, errInvalidDeleteCombo, http.StatusForbidden)
		return
	}

	err = s.Harbor.DeleteRobot(ctx, robotID)
	s.countRobotOp("delete", err)
	if errors.Is(err, harbor.ErrRobotNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		s.Log.ErrorContext(ctx, "delete robot account", slog.Any("error", err))
		http.Error(w, "failed to delete robot account in Harbor", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// countRobotOp records the outcome of a Harbor create or delete call.
// Requests rejected before reaching Harbor are not counted here.
func (s *Server) countRobotOp(op string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
	}
	s.robotOps.WithLabelValues(op, result).Inc()
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Harbor.Ping(r.Context()); err != nil {
		s.Log.ErrorContext(r.Context(), "ping harbor", slog.Any("error", err))
		http.Error(w, "not healthy: failed to ping harbor", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimLeft(token, " ")
	return token, token != ""
}
