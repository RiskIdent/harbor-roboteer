package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const deleteTokenPrefix = "roboteer-delete-token-sha256:"

type postTokenRequest struct {
	// name and project come from the caller until JWT validation lands
	// TODO: once we have implemented jwt remove this
	Name    string `json:"name"`
	Project string `json:"project"`
}

func (r postTokenRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
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

func (s *Server) registerHandlers() {
	router := s.router

	router.HandleFunc("POST /token", s.postToken)
	router.HandleFunc("DELETE /token", s.deleteToken)
}

func (s *Server) postToken(w http.ResponseWriter, r *http.Request) {
	var req postTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "body must be valid JSON", http.StatusBadRequest)
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	deleteToken := rand.Text()
	hash := sha256.Sum256([]byte(deleteToken))

	created, err := s.harbor.CreateRobot(r.Context(), req.Name, req.Project, deleteTokenPrefix+hex.EncodeToString(hash[:]))
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, "invalid delete token", http.StatusForbidden)
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

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimLeft(token, " ")
	return token, token != ""
}
