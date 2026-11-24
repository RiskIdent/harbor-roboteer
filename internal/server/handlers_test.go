package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

type pingHarbor struct {
	harborClient
	err error
}

func (h pingHarbor) Ping(context.Context) error {
	return h.err
}

func TestRoutes(t *testing.T) {
	var logs bytes.Buffer
	hc := &pingHarbor{}
	s, err := NewServer(t.Context(), "127.0.0.1:0", slog.New(slog.NewTextHandler(&logs, nil)), hc, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.listener.Close() })

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/api/roboteer/v1/robot", http.StatusUnauthorized},
		{http.MethodDelete, "/api/roboteer/v1/robot", http.StatusUnauthorized},
		{http.MethodGet, "/api/roboteer/v1/health", http.StatusOK},
		{http.MethodPost, "/api/v1/robot", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/robot", http.StatusNotFound},
		{http.MethodGet, "/api/v1/health", http.StatusNotFound},
		{http.MethodGet, "/api/v2.0/health", http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil))
			if response.Code != test.want {
				t.Errorf("status = %d, want %d", response.Code, test.want)
			}
		})
	}

	logs.Reset()
	hc.err = errors.New("ping failed")
	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/roboteer/v1/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("unhealthy status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if !s.httpMetrics.skip(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/roboteer/v1/health", nil)) {
		t.Error("health path should be excluded from monitoring")
	}
	if bytes.Contains(logs.Bytes(), []byte("msg=request")) {
		t.Error("health request should not be logged")
	}
}

func TestBuildRobotCreate(t *testing.T) {
	fixedNow := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	setTime(t, fixedNow)

	cfg := mustParseConfig(t, `
		local lib = {
		  project(name, access):: { kind: 'project', namespace: name, access: access },
		  allProjects(access):: self.project('*', access),
		  system(access):: { kind: 'system', access: access },
		};
		function(claims) [
		  lib.project(std.split(claims.repository, '/')[1], { resource: ['project-action'] }),
		  lib.project(std.split(claims.repository, '/')[1], { resource: ['role-action', 'project-action'] }),
		  lib.allProjects({ resource: ['all-project-action'] }),
		  lib.system({ resource: ['system-action'] }),
		  if claims.repository == 'other/repo' then lib.system({ resource: ['other-action'] }),
		]
	`)
	token := jwt.New()
	if err := token.Set("repository", "acme/api"); err != nil {
		t.Fatal(err)
	}
	if err := token.Set("sub", "oidc-user"); err != nil {
		t.Fatal(err)
	}

	got, _, err := buildRobotCreate(robotCreateOptions{
		Config:      cfg,
		Token:       token,
		Subject:     "oidc-user",
		Name:        "requested-name",
		DeleteToken: "delete-token",
		Version:     "v1.2.3",
		RobotTTL:    2 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := harbor.RobotCreate{
		NameBase: "requested-name",
		ProjectPermissions: map[string]harbor.Permissions{
			"api": {"resource": {"project-action", "role-action"}},
		},
		AllProjectPermissions: harbor.Permissions{"resource": {"all-project-action"}},
		SystemPermissions:     harbor.Permissions{"resource": {"system-action"}},
		Expiry:                fixedNow.Add(2 * time.Hour),
		Description:           `Robot account for OIDC user "oidc-user"`,
		DeleteToken:           "sha256:798a0ba5da468a14f0f9a0269b2f0ee010ff23b764d0e2ddd83381590da87bb0",
		ManagedBy:             "harbor-roboteer v1.2.3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wrong robot create options\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildRobotCreateErrors(t *testing.T) {
	token := jwt.New()
	if err := token.Set("repository", "contoso/api"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		config *config.Config
		want   error
	}{
		{
			name:   "no permissions",
			config: mustParseConfig(t, `function(claims) [if claims.repository == 'acme/api' then {}]`),
			want:   errNoPermissions,
		},
		{
			name:   "evaluate",
			config: mustParseConfig(t, `function(claims) [{ kind: 'unknown', access: { resource: ['action'] } }]`),
			want:   errEvaluateConfig,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := buildRobotCreate(robotCreateOptions{
				Config: test.config,
				Token:  token,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("buildRobotCreate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func mustParseConfig(t *testing.T, source string) *config.Config {
	t.Helper()
	cfg, err := config.Parse("test.jsonnet", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func setTime(t *testing.T, value time.Time) {
	t.Helper()
	t.Setenv("__HARBOR_ROBOTEER_TEST_TIME", "set") // this calls t.checkParallel(), to reject calls from parallel tests
	original := now
	now = func() time.Time { return value }
	t.Cleanup(func() { now = original })
}
