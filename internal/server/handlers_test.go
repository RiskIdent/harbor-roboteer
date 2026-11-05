package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RiskIdent/harbor-roboteer/api"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

const testVersion = "v1.2.3-test"

type fakeHarbor struct {
	robot   *models.Robot
	deleted []int64
}

func (f *fakeHarbor) CreateRobot(context.Context, string, string, string) (*models.RobotCreated, error) {
	return nil, nil
}

func (f *fakeHarbor) GetRobotByID(context.Context, int64) (*models.Robot, error) {
	return f.robot, nil
}

func (f *fakeHarbor) DeleteRobot(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func serve(t *testing.T, hc harborClient, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	s, err := NewServer(t.Context(), ":0", slog.New(slog.DiscardHandler), testVersion, hc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.listener.Close() })
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func TestDeleteToken(t *testing.T) {
	const deleteKey = "secret-delete-key"
	hash := sha256.Sum256([]byte(deleteKey))
	hc := &fakeHarbor{robot: &models.Robot{Description: deleteTokenPrefix + hex.EncodeToString(hash[:])}}

	tests := []struct {
		name       string
		target     string
		auth       string
		wantStatus int
		wantDelete bool
	}{
		{"no auth header", "/token?robot_id=1", "", http.StatusUnauthorized, false},
		{"wrong auth scheme", "/token?robot_id=1", "Basic abc", http.StatusUnauthorized, false},
		{"missing robot_id", "/token", "Bearer " + deleteKey, http.StatusBadRequest, false},
		{"non-integer robot_id", "/token?robot_id=abc", "Bearer " + deleteKey, http.StatusBadRequest, false},
		{"wrong delete key", "/token?robot_id=1", "Bearer nope", http.StatusForbidden, false},
		{"ok", "/token?robot_id=1", "Bearer " + deleteKey, http.StatusNoContent, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hc.deleted = nil
			req := httptest.NewRequest(http.MethodDelete, tc.target, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := serve(t, hc, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if tc.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
			}
			if got := len(hc.deleted) == 1; got != tc.wantDelete {
				t.Errorf("robot deleted = %v, want %v", got, tc.wantDelete)
			}
		})
	}
}

func TestPostTokenRejectsMissingJWT(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(`{"project":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := serve(t, &fakeHarbor{}, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
	}
}

func TestGetOpenAPISpec(t *testing.T) {
	rec := serve(t, &fakeHarbor{}, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var spec struct {
		OpenAPI string                    `json:"openapi"`
		Info    struct{ Version string }  `json:"info"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if spec.OpenAPI == "" {
		t.Error("openapi version is empty")
	}
	if spec.Info.Version != testVersion {
		t.Errorf("info.version = %q, want the app version %q", spec.Info.Version, testVersion)
	}
	if _, ok := spec.Paths["/token"]["post"]; !ok {
		t.Error("spec is missing POST /token")
	}
}

func TestDocs(t *testing.T) {
	tests := []struct {
		target       string
		wantStatus   int
		wantLocation string
		wantInBody   string
	}{
		{"/", http.StatusTemporaryRedirect, "/docs/", ""},
		{"/unknown", http.StatusNotFound, "", ""},
		{"/docs", http.StatusTemporaryRedirect, "/docs/", ""},
		{"/docs/", http.StatusOK, "", `spec-url="../openapi.yaml"`},
		{"/docs/redoc.standalone.js", http.StatusOK, "", "Redoc"},
		{"/docs/nope.js", http.StatusNotFound, "", ""},
		{"/docs/README.md", http.StatusNotFound, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			rec := serve(t, &fakeHarbor{}, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("Location"); got != tc.wantLocation {
				t.Errorf("Location = %q, want %q", got, tc.wantLocation)
			}
			if !strings.Contains(rec.Body.String(), tc.wantInBody) {
				t.Errorf("body does not contain %q", tc.wantInBody)
			}
		})
	}
}

func TestGetOpenAPISpecYAML(t *testing.T) {
	rec := serve(t, &fakeHarbor{}, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", got)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "openapi: ") {
		t.Errorf("body doesn't look like the spec source: %.40q", body)
	}
	// docs list operations in source order, so POST must stay before DELETE
	if post, del := strings.Index(body, "\n    post:"), strings.Index(body, "\n    delete:"); post < 0 || del < 0 || post > del {
		t.Errorf("want post before delete, got post=%d delete=%d", post, del)
	}
}

func TestOpenAPIYAMLUsesAppVersion(t *testing.T) {
	rec := serve(t, &fakeHarbor{}, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	// Quoted, so that versions like "1.10" stay strings
	line := `  version: "` + testVersion + `"`
	if !strings.Contains(rec.Body.String(), line) {
		t.Fatalf("served spec is missing %q", line)
	}
	// Apart from the version, nothing may differ from the source
	got := strings.Replace(rec.Body.String(), line, "  version: 0.0.0", 1)
	if got != string(api.Spec) {
		t.Error("served spec differs from api/openapi.yaml in more than the version")
	}
}
