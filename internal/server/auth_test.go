package server

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RiskIdent/harbor-roboteer/internal/server/oapi"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
)

// testSpec returns a spec with the given security-related YAML added to a
// single operation, "getThing", using the schemes the server supports.
func testSpec(t *testing.T, global, operation string) *openapi3.T {
	t.Helper()
	spec, err := openapi3.NewLoader().LoadFromData([]byte(`
openapi: 3.0.3
info: {title: test, version: "1"}
` + global + `
paths:
  /thing:
    get:
      operationId: getThing
` + operation + `
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    JWT: {type: http, scheme: bearer}
    DeleteKey: {type: http, scheme: bearer}
    Gamma: {type: http, scheme: bearer}
`))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestOperationSchemesReadsSecurityFromSpec(t *testing.T) {
	spec, err := oapi.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	got, err := operationSchemes(spec)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]securityScheme{
		"PostToken":   schemeJWT,
		"DeleteToken": schemeDeleteKey,
		"GetHealth":   schemeAnonymous,
	}
	if !maps.Equal(got, want) {
		t.Errorf("schemes by operation = %v, want %v", got, want)
	}
}

func TestOperationSchemes(t *testing.T) {
	tests := []struct {
		name       string
		global     string
		operation  string
		wantScheme securityScheme
		wantErr    string
	}{
		{name: "no security is public", wantScheme: schemeAnonymous},
		{name: "operation scheme", operation: "      security: [{JWT: []}]", wantScheme: schemeJWT},
		{name: "global scheme is the default", global: "security: [{DeleteKey: []}]", wantScheme: schemeDeleteKey},
		{name: "operation overrides global", global: "security: [{DeleteKey: []}]", operation: "      security: [{JWT: []}]", wantScheme: schemeJWT},
		{name: "empty security makes it public", global: "security: [{DeleteKey: []}]", operation: "      security: []", wantScheme: schemeAnonymous},
		{name: "unsupported scheme", operation: "      security: [{Gamma: []}]", wantErr: `"Gamma" is not supported`},
		{name: "alternative schemes", operation: "      security: [{JWT: []}, {DeleteKey: []}]", wantErr: "only a single security scheme"},
		{name: "combined schemes", operation: "      security: [{JWT: [], DeleteKey: []}]", wantErr: "only a single security scheme"},
		{name: "optional authentication", operation: "      security: [{JWT: []}, {}]", wantErr: "only a single security scheme"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := operationSchemes(testSpec(t, tc.global, tc.operation))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scheme, ok := got["GetThing"]; !ok || scheme != tc.wantScheme {
				t.Errorf("scheme = %v (found: %t), want %v", scheme, ok, tc.wantScheme)
			}
		})
	}
}

// Guards against a securityScheme that authenticateScheme doesn't handle.
// It goes through every value that has a name in String.
func TestAuthenticateSchemeHandlesAllSchemes(t *testing.T) {
	s := &Server{log: slog.New(slog.DiscardHandler)}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil) // without credentials
	for scheme := schemeJWT; !strings.HasPrefix(scheme.String(), "securityScheme("); scheme++ {
		t.Run(scheme.String(), func(t *testing.T) {
			// A panic here means the scheme isn't handled
			if _, err := s.authenticateScheme(t.Context(), req, scheme); err == nil {
				t.Error("want the request without credentials to be rejected")
			}
		})
	}
}

func TestAuthenticateSchemePanicsForSchemesWithoutAuthentication(t *testing.T) {
	for _, scheme := range []securityScheme{schemeInvalid, schemeAnonymous, securityScheme(99)} {
		t.Run(scheme.String(), func(t *testing.T) {
			s := &Server{log: slog.New(slog.DiscardHandler)}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			defer func() {
				if recover() == nil {
					t.Error("want a panic, instead of letting the request through")
				}
			}()
			_, _ = s.authenticateScheme(t.Context(), req, scheme)
		})
	}
}

func TestAuthenticatePanicsForUnknownOperation(t *testing.T) {
	s := &Server{schemes: map[string]securityScheme{}}
	next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		t.Error("the handler ran for an operation without security configuration")
		return nil, nil
	}

	defer func() {
		if recover() == nil {
			t.Error("want a panic for an operation without security configuration")
		}
	}()
	s.authenticate(next, "SomethingNew")
}

func TestOnlyKey(t *testing.T) {
	if k, ok := onlyKey(map[string]int{"only": 1}); !ok || k != "only" {
		t.Errorf("onlyKey(single) = %q, %t, want %q, true", k, ok, "only")
	}
	if k, ok := onlyKey(map[string]int{"a": 1, "b": 2}); ok || k != "" {
		t.Errorf("onlyKey(several) = %q, %t, want the zero value and false", k, ok)
	}
	if k, ok := onlyKey(map[string]int{}); ok || k != "" {
		t.Errorf("onlyKey(empty) = %q, %t, want the zero value and false", k, ok)
	}
	if _, ok := onlyKey[string, int](nil); ok {
		t.Error("onlyKey(nil) = true, want false")
	}
}

// The generated code passes names made by oapi-codegen to authenticate, so
// operationName must give the same names. Only the tests import the generator.
func TestOperationNameMatchesGenerator(t *testing.T) {
	spec, err := oapi.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"getThing", "postHTTPThing", "list2Things", "a", "X"}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			ids = append(ids, op.OperationID)
		}
	}

	for _, id := range ids {
		if got, want := operationName(id), codegen.ToCamelCase(id); got != want {
			t.Errorf("operationName(%q) = %q, but oapi-codegen generates %q", id, got, want)
		}
	}
}
