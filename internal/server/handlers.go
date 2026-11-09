package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/server/oapi"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const deleteTokenPrefix = "roboteer-delete-token-sha256:"

// invalidDeleteCombo is the response for both an unknown robot ID and a wrong
// delete token, so that callers can't probe which robot IDs exist.
const invalidDeleteCombo = "invalid robot ID and delete token combination"

// untracedPaths are not traced, as they are polled or static.
// A "*" at the end matches any path with that prefix.
var untracedPaths = []string{
	"/health",
	"/openapi.json",
	"/openapi.yaml",
	"/",
	"/docs",
	"/docs/*",
}

func (s *Server) registerHandlers(appVersion string) error {
	specJSON, err := openAPIJSON(appVersion)
	if err != nil {
		return err
	}
	specYAML, err := openAPIYAML(appVersion)
	if err != nil {
		return err
	}
	s.router.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(specJSON)
	})
	// The docs use this one: unlike the JSON, which is re-marshalled and so
	// sorts methods alphabetically, it lists operations in the order of the source
	s.router.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(specYAML)
	})
	// "{$}" matches only the root path, not every unmatched path
	s.router.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusTemporaryRedirect)
	})
	// "GET /docs" is redirected to "/docs/" by the mux
	s.router.Handle("GET /docs/", docsHandler())

	spec, err := oapi.GetSpec()
	if err != nil {
		return fmt.Errorf("load embedded OpenAPI spec: %w", err)
	}
	s.schemes, err = operationSchemes(spec)
	if err != nil {
		return fmt.Errorf("apply security from OpenAPI spec: %w", err)
	}

	strict := oapi.NewStrictHandler(s, []oapi.StrictMiddlewareFunc{s.authenticate})
	oapi.HandlerWithOptions(strict, oapi.StdHTTPServerOptions{BaseRouter: s.router})
	return nil
}

func unauthorized(msg string) oapi.UnauthorizedTextResponse {
	scheme := "Bearer"
	return oapi.UnauthorizedTextResponse{
		Body:    msg,
		Headers: oapi.UnauthorizedResponseHeaders{WWWAuthenticate: &scheme},
	}
}

// GetHealth is the handler for "GET /health"
func (s *Server) GetHealth(ctx context.Context, _ oapi.GetHealthRequestObject) (oapi.GetHealthResponseObject, error) {
	if err := s.harbor.Ping(ctx); err != nil {
		s.log.ErrorContext(ctx, "ping harbor", slog.Any("error", err))
		return oapi.GetHealth503TextResponse("not healthy"), nil
	}
	return oapi.GetHealth200Response{}, nil
}

// PostToken is the handler for "POST /token"
func (s *Server) PostToken(ctx context.Context, request oapi.PostTokenRequestObject) (oapi.PostTokenResponseObject, error) {
	tok := contextJWT(ctx)
	req := request.Body

	if err := validation.ValidateStruct(req,
		validation.Field(&req.Project, validation.Required),
	); err != nil {
		return oapi.PostToken400TextResponse(err.Error()), nil
	}

	var name string
	if req.Name != nil {
		name = *req.Name
	}
	if name == "" {
		name, _ = tok.Subject()
	}
	if name == "" {
		s.log.ErrorContext(ctx, "token has no sub claim and no name was provided in the request body")
		return oapi.PostToken401TextResponse{UnauthorizedTextResponse: unauthorized("token has no sub claim, set name in the request body")}, nil
	}

	deleteToken := rand.Text()
	hash := sha256.Sum256([]byte(deleteToken))

	created, err := s.harbor.CreateRobot(ctx, name, req.Project, deleteTokenPrefix+hex.EncodeToString(hash[:]))
	if errors.Is(err, harbor.ErrRobotExists) {
		return oapi.PostToken409TextResponse(err.Error()), nil
	}
	if err != nil {
		s.log.ErrorContext(ctx, "create robot account", slog.Any("error", err))
		return oapi.PostToken502TextResponse("failed to create robot account in Harbor"), nil
	}

	return oapi.PostToken200JSONResponse{
		Robot: oapi.RobotCredentials{
			Id:       created.ID,
			Username: created.Name,
			Password: created.Secret,
		},
		DeleteToken: deleteToken,
	}, nil
}

// DeleteToken is the handler for "DELETE /token"
func (s *Server) DeleteToken(ctx context.Context, request oapi.DeleteTokenRequestObject) (oapi.DeleteTokenResponseObject, error) {
	token := contextDeleteToken(ctx)
	robotID := request.Params.RobotId

	rb, err := s.harbor.GetRobotByID(ctx, robotID)
	if errors.Is(err, harbor.ErrRobotNotFound) {
		// Same as for a wrong delete token, so that robot IDs can't be probed
		return oapi.DeleteToken403TextResponse(invalidDeleteCombo), nil
	}
	if err != nil {
		s.log.ErrorContext(ctx, "get robot account", slog.Any("error", err))
		return oapi.DeleteToken502TextResponse("failed to get robot account from Harbor"), nil
	}

	hash := sha256.Sum256([]byte(token))
	want := deleteTokenPrefix + hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(rb.Description), []byte(want)) != 1 {
		return oapi.DeleteToken403TextResponse(invalidDeleteCombo), nil
	}

	err = s.harbor.DeleteRobot(ctx, robotID)
	if errors.Is(err, harbor.ErrRobotNotFound) {
		return oapi.DeleteToken404TextResponse(err.Error()), nil
	}
	if err != nil {
		s.log.ErrorContext(ctx, "delete robot account", slog.Any("error", err))
		return oapi.DeleteToken502TextResponse("failed to delete robot account in Harbor"), nil
	}
	return oapi.DeleteToken204Response{}, nil
}
