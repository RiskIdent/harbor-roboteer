package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

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
	// TODO: implement this
}
