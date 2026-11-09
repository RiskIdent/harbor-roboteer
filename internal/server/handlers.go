package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/server/oapi"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

const deleteTokenPrefix = "roboteer-delete-token-sha256:"

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

	strict := oapi.NewStrictHandler(s, []oapi.StrictMiddlewareFunc{s.authenticate})
	oapi.HandlerWithOptions(strict, oapi.StdHTTPServerOptions{BaseRouter: s.router})
	return nil
}

type (
	jwtCtxKey       struct{}
	deleteKeyCtxKey struct{}
)

// contextJWT returns the token that [Server.authenticate] has verified.
// It panics if there is none, as that means the middleware isn't registered.
func contextJWT(ctx context.Context) jwt.Token {
	tok, ok := ctx.Value(jwtCtxKey{}).(jwt.Token)
	if !ok {
		panic("no verified JWT in context, is the authenticate middleware registered?")
	}
	return tok
}

// contextDeleteToken returns the delete token that [Server.authenticate] has
// extracted. It panics if there is none, as that means the middleware isn't
// registered.
func contextDeleteToken(ctx context.Context) string {
	token, ok := ctx.Value(deleteKeyCtxKey{}).(string)
	if !ok {
		panic("no delete token in context, is the authenticate middleware registered?")
	}
	return token
}

func unauthorized(msg string) oapi.UnauthorizedTextResponse {
	scheme := "Bearer"
	return oapi.UnauthorizedTextResponse{
		Body:    msg,
		Headers: oapi.UnauthorizedResponseHeaders{WWWAuthenticate: &scheme},
	}
}

// authenticate enforces the security schemes declared in api/openapi.yaml.
// The generated code does not do it for us, so it is keyed on the generated
// operation names (Go-cased operationId from the spec).
func (s *Server) authenticate(next oapi.StrictHandlerFunc, operationID string) oapi.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		switch operationID {
		case "PostToken": // security: JWT
			tok, err := jwt.ParseRequest(r, s.jwtOpts...)
			if err != nil {
				s.log.ErrorContext(ctx, "parse token")
				s.log.DebugContext(ctx, "reject token", slog.Any("error", err))
				return oapi.PostToken401TextResponse{UnauthorizedTextResponse: unauthorized("invalid or missing token")}, nil
			}
			ctx = context.WithValue(ctx, jwtCtxKey{}, tok)
		case "DeleteToken": // security: DeleteKey
			token, ok := bearerToken(r)
			if !ok {
				return oapi.DeleteToken401TextResponse{UnauthorizedTextResponse: unauthorized("Authorization: Bearer <delete token> header is required")}, nil
			}
			ctx = context.WithValue(ctx, deleteKeyCtxKey{}, token)
		}
		return next(ctx, w, r, req)
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
		return oapi.DeleteToken404TextResponse(err.Error()), nil
	}
	if err != nil {
		s.log.ErrorContext(ctx, "get robot account", slog.Any("error", err))
		return oapi.DeleteToken502TextResponse("failed to get robot account from Harbor"), nil
	}

	hash := sha256.Sum256([]byte(token))
	want := deleteTokenPrefix + hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(rb.Description), []byte(want)) != 1 {
		return oapi.DeleteToken403TextResponse("invalid delete token"), nil
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

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimLeft(token, " ")
	return token, token != ""
}
