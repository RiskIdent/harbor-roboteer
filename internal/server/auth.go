package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/RiskIdent/harbor-roboteer/internal/server/oapi"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

// Authentication is driven by the "security" of each operation in
// api/openapi.yaml. The generated code doesn't enforce it, so
// [operationSchemes] reads it from the spec, and [Server.authenticate] applies
// it per request, using [Server.authenticateScheme].

// securityScheme is how an operation is authenticated. The zero value is
// invalid on purpose, so that an unset value is never treated as public.
type securityScheme int

const (
	schemeInvalid   securityScheme = iota
	schemeAnonymous                // no authentication
	schemeJWT                      // the "JWT" securityScheme in the spec
	schemeDeleteKey                // the "DeleteKey" securityScheme in the spec
)

func (s securityScheme) String() string {
	switch s {
	case schemeInvalid:
		return "invalid"
	case schemeAnonymous:
		return "anonymous"
	case schemeJWT:
		return "JWT"
	case schemeDeleteKey:
		return "DeleteKey"
	default:
		return fmt.Sprintf("securityScheme(%d)", int(s))
	}
}

// operationName returns the name that oapi-codegen generates for an operation,
// which is what the generated code passes to [Server.authenticate].
//
// It mirrors [codegen.ToCamelCase], the default "name-normalizer", for
// operationIds made of only letters and digits, such as "postToken".
// That function isn't imported here, as the whole generator would then end up
// in the binary. A test checks that the two agree.
//
// [codegen.ToCamelCase]: https://pkg.go.dev/github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen#ToCamelCase
func operationName(operationID string) string {
	if operationID == "" {
		return ""
	}
	return strings.ToUpper(operationID[:1]) + operationID[1:]
}

// schemeFor maps the security requirements of an operation to a securityScheme,
// by the name of the securityScheme in the spec
func schemeFor(reqs openapi3.SecurityRequirements) (securityScheme, error) {
	if len(reqs) == 0 {
		return schemeAnonymous, nil
	}
	name, ok := onlyKey(reqs[0])
	if len(reqs) > 1 || !ok {
		return schemeInvalid, errors.New("only a single security scheme per operation is supported")
	}
	switch name {
	case "JWT":
		return schemeJWT, nil
	case "DeleteKey":
		return schemeDeleteKey, nil
	default:
		return schemeInvalid, fmt.Errorf("security scheme %q is not supported", name)
	}
}

type (
	jwtCtxKey       struct{}
	deleteKeyCtxKey struct{}
)

// contextJWT returns the token that [Server.authenticateScheme] has verified.
// It panics if there is none, as that means the middleware isn't registered.
func contextJWT(ctx context.Context) jwt.Token {
	tok, ok := ctx.Value(jwtCtxKey{}).(jwt.Token)
	if !ok {
		panic("no verified JWT in context, is the authenticate middleware registered?")
	}
	return tok
}

// contextDeleteToken returns the delete token that [Server.authenticateScheme]
// has extracted. It panics if there is none, as that means the middleware isn't
// registered.
func contextDeleteToken(ctx context.Context) string {
	token, ok := ctx.Value(deleteKeyCtxKey{}).(string)
	if !ok {
		panic("no delete token in context, is the authenticate middleware registered?")
	}
	return token
}

// authenticateScheme checks the request against the security scheme.
// The returned context carries what the handlers need, see [contextJWT] and
// [contextDeleteToken]. The error message is sent to the client, so it must not
// hold any secrets.
func (s *Server) authenticateScheme(ctx context.Context, r *http.Request, scheme securityScheme) (context.Context, error) {
	switch scheme {
	case schemeJWT:
		tok, err := jwt.ParseRequest(r, s.jwtOpts...)
		if err != nil {
			s.log.ErrorContext(ctx, "parse token") // without "err", as we're worried it could expose sensitive info
			s.log.DebugContext(ctx, "reject token", slog.Any("error", err))
			return ctx, errors.New("invalid or missing token")
		}
		return context.WithValue(ctx, jwtCtxKey{}, tok), nil

	case schemeDeleteKey:
		token, ok := bearerToken(r)
		if !ok {
			return ctx, errors.New("an Authorization: Bearer <delete token> header is required")
		}
		return context.WithValue(ctx, deleteKeyCtxKey{}, token), nil

	default:
		// operationSchemes only returns schemes that are handled above
		panic(fmt.Errorf("cannot authenticate with security scheme %v", scheme))
	}
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimLeft(token, " ")
	return token, token != ""
}

// operationSchemes returns the security scheme that each operation in the spec
// requires, keyed by the generated name of the operation, see [operationName].
//
// It fails on any security setup that it doesn't implement, rather than
// leaving the operation unprotected.
func operationSchemes(spec *openapi3.T) (map[string]securityScheme, error) {
	result := map[string]securityScheme{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" {
				return nil, fmt.Errorf("%s %s: missing operationId", method, path)
			}
			reqs := spec.Security // the default, unless the operation has its own
			if op.Security != nil {
				reqs = *op.Security
			}
			scheme, err := schemeFor(reqs)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, path, err)
			}
			result[operationName(op.OperationID)] = scheme
		}
	}
	return result, nil
}

// authenticate enforces the security of each operation, as described in the spec.
func (s *Server) authenticate(next oapi.StrictHandlerFunc, operationID string) oapi.StrictHandlerFunc {
	scheme, known := s.schemes[operationID]
	switch {
	case !known:
		// e.g. if the generated operation name doesn't match the spec
		panic(fmt.Errorf("no security configuration for operation %q", operationID))

	case scheme == schemeAnonymous:
		return next // no-op

	default:
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			var err error
			ctx, err = s.authenticateScheme(ctx, r, scheme)
			if err != nil {
				// Written here, as the typed 401 responses are specific to each operation.
				// The strict handler does nothing more when the response is nil.
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return nil, nil
			}
			return next(ctx, w, r, req)
		}
	}
}

// onlyKey returns the key of a map that has exactly one key,
// or false if the map has none or several.
func onlyKey[K comparable, V any](m map[K]V) (K, bool) {
	var key K
	if len(m) != 1 {
		return key, false
	}
	for k := range m {
		key = k
	}
	return key, true
}
