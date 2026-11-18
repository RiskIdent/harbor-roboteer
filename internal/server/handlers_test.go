package server

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

func TestBuildRobotCreate(t *testing.T) {
	fixedNow := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	setTime(t, fixedNow)

	cfg := &config.Config{
		Identities: []config.Identity{{
			"repository": config.MustPattern("acme/{repo}"),
		}},
		Projects: config.Projects{
			Grants: map[string]config.ProjectGrant{
				"project": {
					Projects: []config.Pattern{config.MustPattern("{repo}")},
					Permissions: config.ProjectPermissions{
						"resource": {"project-action"},
					},
				},
			},
		},
		AllProjects: config.AllProjects{
			Grants: map[string]config.AllProjectGrant{
				"all projects": {
					Permissions: config.ProjectPermissions{
						"resource": {"all-project-action"},
					},
				},
			},
		},
		System: config.System{
			Grants: map[string]config.SystemGrant{
				"system": {
					Permissions: config.SystemPermissions{
						"resource": {"system-action"},
					},
				},
			},
		},
	}
	token := jwt.New()
	if err := token.Set("repository", "acme/api"); err != nil {
		t.Fatal(err)
	}
	if err := token.Set("sub", "oidc-user"); err != nil {
		t.Fatal(err)
	}

	got, err := buildRobotCreate(robotCreateOptions{
		Config:      cfg,
		Token:       token,
		Name:        "requested-name",
		DeleteToken: "delete-token",
		Version:     "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := harbor.RobotCreate{
		NameBase: "requested-name",
		ProjectPermissions: map[string]harbor.Permissions{
			"api": {"resource": {"project-action"}},
		},
		AllProjectPermissions: harbor.Permissions{"resource": {"all-project-action"}},
		SystemPermissions:     harbor.Permissions{"resource": {"system-action"}},
		EarlyExpiry:           fixedNow.Add(time.Hour),
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
			name: "identify",
			config: &config.Config{Identities: []config.Identity{{
				"repository": config.MustPattern("acme/{repo}"),
			}}},
			want: errIdentifyClaims,
		},
		{
			name: "evaluate",
			config: &config.Config{
				Identities: []config.Identity{{
					"repository": config.MustPattern("contoso/{repo}"),
				}},
				Projects: config.Projects{Grants: map[string]config.ProjectGrant{
					"missing role": {
						Projects: []config.Pattern{config.MustPattern("{repo}")},
						Roles:    []config.Pattern{config.MustPattern("missing")},
					},
				}},
			},
			want: errEvaluateConfig,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildRobotCreate(robotCreateOptions{
				Config: test.config,
				Token:  token,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("buildRobotCreate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func setTime(t *testing.T, value time.Time) {
	t.Helper()
	t.Setenv("__HARBOR_ROBOTEER_TEST_TIME", "set") // this calls t.checkParallel(), to reject calls from parallel tests
	original := now
	now = func() time.Time { return value }
	t.Cleanup(func() { now = original })
}
