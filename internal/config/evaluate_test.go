package config

import (
	"iter"
	"maps"
	"reflect"
	"testing"
)

func claimSeq(claims map[string]any) iter.Seq2[string, any] {
	return maps.All(claims)
}

func TestIdentify(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		claims map[string]any
		want   map[string]string
	}{
		{
			name: "matches identity and captures placeholders",
			config: Config{
				KnownClaims: []string{"repository_owner", "repository", "environment"},
				Identities: []Identity{{
					"repository_owner": mustPattern(t, "acme"),
					"repository":       mustPattern(t, "acme/{repo}"),
				}},
			},
			claims: map[string]any{
				"repository_owner": "acme",
				"repository":       "acme/api",
				"environment":      "production",
				"ignored":          []any{"ignored", 1},
			},
			want: map[string]string{
				"repository_owner": "acme",
				"repository":       "acme/api",
				"environment":      "production",
				"repo":             "api",
			},
		},
		{
			name: "uses first matching identity",
			config: Config{
				Identities: []Identity{
					{"repository": mustPattern(t, "acme/{repo}")},
					{
						"repository":  mustPattern(t, "acme/{project}"),
						"environment": mustPattern(t, "harbor"),
					},
				},
			},
			claims: map[string]any{
				"repository":  "acme/api",
				"environment": "harbor",
			},
			want: map[string]string{
				"repository":  "acme/api",
				"environment": "harbor",
				"repo":        "api",
			},
		},
		{
			name: "uses grant whens without identities",
			config: Config{
				Projects: Projects{Grants: map[string]ProjectGrant{
					"pull": {When: When{"sub": "repo:acme/api:pull"}},
				}},
			},
			claims: map[string]any{"sub": "repo:acme/api:pull", "exp": 123},
			want:   map[string]string{"sub": "repo:acme/api:pull"},
		},
		{
			name: "empty identity matches",
			config: Config{
				Projects: Projects{Grants: map[string]ProjectGrant{
					"all": {},
				}},
			},
			claims: map[string]any{"ignored": true},
			want:   map[string]string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.config.Identify(claimSeq(test.claims))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("wrong result\nwant: %#v\ngot:  %#v", test.want, got)
			}
		})
	}
}

func TestIdentifyErrors(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		claims map[string]any
	}{
		{
			name: "non-string known claim",
			config: Config{
				KnownClaims: []string{"groups"},
				Identities:  []Identity{{"groups": mustPattern(t, "{group}")}},
			},
			claims: map[string]any{"groups": []any{"developers"}},
		},
		{
			name: "missing claim",
			config: Config{
				Identities: []Identity{{"repository": mustPattern(t, "acme/{repo}")}},
			},
			claims: map[string]any{"repository_owner": "acme"},
		},
		{
			name: "pattern does not match",
			config: Config{
				Identities: []Identity{{"repository": mustPattern(t, "acme/{repo}")}},
			},
			claims: map[string]any{"repository": "contoso/api"},
		},
		{
			name: "capture conflicts with claim",
			config: Config{
				Identities: []Identity{{
					"first":  mustPattern(t, "{second}"),
					"second": mustPattern(t, "literal"),
				}},
			},
			claims: map[string]any{"first": "captured", "second": "literal"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, err := test.config.Identify(claimSeq(test.claims)); err == nil {
				t.Fatalf("Identify() = %#v, want error", got)
			}
		})
	}
}

func TestEvaluateSystemPermissions(t *testing.T) {
	config := Config{
		System: System{
			Roles: map[string]SystemPermissions{
				"bootstrap": {"resource-a": {"action-from-bootstrap-role"}},
			},
			Grants: map[string]SystemGrant{
				"create project": {
					When:  When{"repository": "acme/bootstrap"},
					Roles: []Pattern{mustPattern(t, "{role}")},
					Permissions: SystemPermissions{
						"resource-a": {"action-from-create-project-grant"},
					},
				},
				"labels": {
					When: When{"repository": "acme/bootstrap"},
					Permissions: SystemPermissions{
						"resource-a": {"action-from-labels-grant"},
						"resource-b": {"shared-action"},
					},
				},
				"unused": {
					When:        When{"repository": "acme/other"},
					Permissions: SystemPermissions{"resource-a": {"action-from-unused"}},
				},
			},
		},
	}

	got, err := config.Evaluate(map[string]string{
		"repository": "acme/bootstrap",
		"role":       "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := SystemPermissions{
		"resource-a": {
			"action-from-bootstrap-role",
			"action-from-create-project-grant",
			"action-from-labels-grant",
		},
		"resource-b": {"shared-action"},
	}
	if !reflect.DeepEqual(got.SystemPermissions, want) {
		t.Errorf("wrong system permissions\nwant: %#v\ngot:  %#v", want, got.SystemPermissions)
	}
}

func TestEvaluateProjectPermissions(t *testing.T) {
	config := Config{
		Projects: Projects{
			Roles: map[string]ProjectPermissions{
				"pull": {
					"resource-a": {"action-from-pull-role"},
					"resource-b": {"shared-action"},
				},
			},
			Grants: map[string]ProjectGrant{
				"own project": {
					When:     When{"environment": "harbor"},
					Projects: []Pattern{mustPattern(t, "{repository_owner}-{repo}")},
					Roles:    []Pattern{mustPattern(t, "{role}")},
					Permissions: ProjectPermissions{
						"resource-a": {"action-from-own-project-grant"},
					},
				},
				"shared project": {
					When:     When{"repository": "acme/api"},
					Projects: []Pattern{mustPattern(t, "{repository_owner}-{repo}")},
					Permissions: ProjectPermissions{
						"resource-a": {"action-from-shared-project-grant"},
						"resource-c": {"shared-action"},
					},
				},
				"unused": {
					When:        When{"environment": "production"},
					Projects:    []Pattern{mustPattern(t, "unrelated")},
					Permissions: ProjectPermissions{"resource-a": {"action-from-unused"}},
				},
			},
		},
	}

	got, err := config.Evaluate(map[string]string{
		"repository":       "acme/api",
		"repository_owner": "acme",
		"repo":             "api",
		"role":             "pull",
		"environment":      "harbor",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ProjectPermissions{
		"acme-api": {
			"resource-a": {
				"action-from-own-project-grant",
				"action-from-pull-role",
				"action-from-shared-project-grant",
			},
			"resource-b": {"shared-action"},
			"resource-c": {"shared-action"},
		},
	}
	if !reflect.DeepEqual(got.ProjectPermissions, want) {
		t.Errorf("wrong project permissions\nwant: %#v\ngot:  %#v", want, got.ProjectPermissions)
	}
}

func TestEvaluateAllProjectPermissions(t *testing.T) {
	config := Config{
		Projects: Projects{
			Roles: map[string]ProjectPermissions{
				"scan": {"resource-a": {"action-from-scan-role"}},
			},
		},
		AllProjects: AllProjects{
			Grants: map[string]AllProjectGrant{
				"scan": {
					When:  When{"repository": "acme/scanner"},
					Roles: []Pattern{mustPattern(t, "{role}")},
					Permissions: ProjectPermissions{
						"resource-a": {"action-from-scan-grant"},
					},
				},
				"pull": {
					When: When{"repository": "acme/scanner"},
					Permissions: ProjectPermissions{
						"resource-a": {"action-from-pull-grant"},
						"resource-b": {"shared-action"},
					},
				},
				"unused": {
					When:        When{"repository": "acme/api"},
					Permissions: ProjectPermissions{"resource-a": {"action-from-unused"}},
				},
			},
		},
	}

	got, err := config.Evaluate(map[string]string{
		"repository": "acme/scanner",
		"role":       "scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := ProjectPermissions{
		"resource-a": {"action-from-pull-grant", "action-from-scan-grant", "action-from-scan-role"},
		"resource-b": {"shared-action"},
	}
	if !reflect.DeepEqual(got.AllProjectPermissions, want) {
		t.Errorf("wrong all-project permissions\nwant: %#v\ngot:  %#v", want, got.AllProjectPermissions)
	}
}

func TestEvaluateProjectPermissionsError(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		claims map[string]string
	}{
		{
			name: "unknown role",
			config: Config{
				Projects: Projects{Grants: map[string]ProjectGrant{
					"unknown role": {
						Projects: []Pattern{mustPattern(t, "acme")},
						Roles:    []Pattern{mustPattern(t, "missing")},
					},
				}},
			},
		},
		{
			name: "missing project capture",
			config: Config{
				Projects: Projects{Grants: map[string]ProjectGrant{
					"missing capture": {
						Projects: []Pattern{mustPattern(t, "{project}")},
					},
				}},
			},
			claims: map[string]string{"repository": "acme/api"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.config.Evaluate(test.claims); err == nil {
				t.Fatal("Evaluate() returned no error")
			}
		})
	}
}
