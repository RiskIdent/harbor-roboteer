package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

const deleteTokenPrefix = "roboteer-delete-token-sha256:"

// maxBodyBytes is the largest request body that POST /token accepts
const maxBodyBytes = 64 << 10

// errInvalidDeleteCombo is the same for an unknown robot and a wrong delete
// token, so that callers can't use it to find out which robot IDs exist
const errInvalidDeleteCombo = "invalid robot ID and delete token combination"

type postTokenRequest struct {
	Name string `json:"name"`
	// project comes from the caller until the permission config (#38) lands
	// TODO: remove once project permissions are resolved from the token's claims
	Project string `json:"project"`
}

func (r postTokenRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Project, validation.Required),
	)
}

type robotCredentials struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type postTokenResponse struct {
	Robot       robotCredentials `json:"robot"`
	DeleteToken string           `json:"deleteToken"`
}

var untracedPaths = []string{
	"/health",
}

func (s *Server) registerHandlers() {
	router := s.router

	router.HandleFunc("POST /token", s.postToken)
	router.HandleFunc("DELETE /token", s.deleteToken)

	router.HandleFunc("GET /health", s.health)
}

func (s *Server) postToken(w http.ResponseWriter, r *http.Request) {
	tok, err := jwt.ParseRequest(r, s.jwtOpts...)
	if err != nil {
		s.log.ErrorContext(r.Context(), "parse token")
		s.log.DebugContext(r.Context(), "reject token", slog.Any("error", err))
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
	var req postTokenRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "body must be valid JSON", http.StatusBadRequest)
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := req.Name
	if name == "" {
		name, _ = tok.Subject()
	}
	if name == "" {
		s.log.ErrorContext(r.Context(), "token has no sub claim and no name was provided in the request body")
		http.Error(w, "token has no sub claim, set name in the request body", http.StatusBadRequest)
		return
	}

	deleteToken := rand.Text()
	hash := sha256.Sum256([]byte(deleteToken))

	created, err := s.harbor.CreateRobot(r.Context(), name, req.Project, deleteTokenPrefix+hex.EncodeToString(hash[:]))
	if errors.Is(err, harbor.ErrRobotExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "create robot account", slog.Any("error", err))
		http.Error(w, "failed to create robot account in Harbor", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(postTokenResponse{
		Robot: robotCredentials{
			ID:       created.ID,
			Username: created.Name,
			Password: created.Secret,
		},
		DeleteToken: deleteToken,
	})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
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

	rb, err := s.harbor.GetRobotByID(r.Context(), robotID)
	if errors.Is(err, harbor.ErrRobotNotFound) {
		http.Error(w, errInvalidDeleteCombo, http.StatusForbidden)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "get robot account", slog.Any("error", err))
		http.Error(w, "failed to get robot account from Harbor", http.StatusBadGateway)
		return
	}

	hash := sha256.Sum256([]byte(token))
	want := deleteTokenPrefix + hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(rb.Description), []byte(want)) != 1 {
		http.Error(w, errInvalidDeleteCombo, http.StatusForbidden)
		return
	}

	err = s.harbor.DeleteRobot(r.Context(), robotID)
	if errors.Is(err, harbor.ErrRobotNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "delete robot account", slog.Any("error", err))
		http.Error(w, "failed to delete robot account in Harbor", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.harbor.Ping(r.Context()); err != nil {
		s.log.ErrorContext(r.Context(), "ping harbor", slog.Any("error", err))
		http.Error(w, "not healthy", http.StatusServiceUnavailable)
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
