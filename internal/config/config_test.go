package config

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func mustPattern(t *testing.T, s string) Pattern {
	t.Helper()
	p, err := ParsePattern(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(t *testing.T) Config
		wantErr string
	}{
		{
			name: "valid",
			cfg: func(t *testing.T) Config {
				return Config{
					KnownClaims: []string{"repository", "environment"},
					Identities: []Identity{{
						"repository":  mustPattern(t, "acme/{repo}"),
						"environment": mustPattern(t, "harbor"),
					}},
					Projects: Projects{
						Roles: map[string]ProjectPermissions{"pull": {"repository": {"pull"}}},
						Grants: map[string]ProjectGrant{"g": {
							When:     When{"repository": "acme/foo", "environment": "harbor"},
							Projects: []Pattern{mustPattern(t, "{repo}")},
							Roles:    []Pattern{mustPattern(t, "pull")},
						}},
					},
				}
			},
		},
		{
			name: "unknown claim",
			cfg: func(t *testing.T) Config {
				return Config{
					KnownClaims: []string{"repository", "environment"},
					Identities:  []Identity{{"repo": mustPattern(t, "x")}},
				}
			},
			wantErr: "not listed in known_claims",
		},
		{
			name: "unknown role",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x")}},
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						Projects: []Pattern{mustPattern(t, "p")},
						Roles:    []Pattern{mustPattern(t, "nope")},
					}}},
				}
			},
			wantErr: `unknown role "nope"`,
		},
		{
			name: "missing when without identities",
			cfg: func(t *testing.T) Config {
				return Config{System: System{Grants: map[string]SystemGrant{"g": {
					Permissions: SystemPermissions{"project": {"create"}},
				}}}}
			},
			wantErr: "required when no identities",
		},
		{
			name: "all_projects unknown role",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x")}},
					AllProjects: AllProjects{Grants: map[string]AllProjectGrant{"g": {
						Roles: []Pattern{mustPattern(t, "nope")},
					}}},
				}
			},
			wantErr: `unknown role "nope"`,
		},
		{
			name: "grant placeholder not in identities",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x-{repo}")}},
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						Projects:    []Pattern{mustPattern(t, "{owner}")},
						Permissions: ProjectPermissions{"repository": {"pull"}},
					}}},
				}
			},
			wantErr: "unknown placeholder {owner}: not a claim or capture of any identity",
		},
		{
			name: "grant placeholders from different identities",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{
						{"a": mustPattern(t, "x-{repo}")},
						{"b": mustPattern(t, "y-{owner}")},
					},
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						Projects:    []Pattern{mustPattern(t, "{owner}-{repo}")},
						Permissions: ProjectPermissions{"repository": {"pull"}},
					}}},
				}
			},
			wantErr: "projects.grants.g: no single identity provides all placeholders: {owner}, {repo}",
		},
		{
			name: "without identities, placeholders must be claims in when",
			cfg: func(t *testing.T) Config {
				return Config{
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						When:        When{"a": "x"},
						Projects:    []Pattern{mustPattern(t, "{a}-{b}")},
						Permissions: ProjectPermissions{"repository": {"pull"}},
					}}},
				}
			},
			wantErr: `projects.grants.g: with no identities configured, placeholders must be claims set in the "when" field: {a}, {b}`,
		},
		{
			name: "without identities, placeholder claim in when",
			cfg: func(t *testing.T) Config {
				return Config{
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						When:        When{"a": "x"},
						Projects:    []Pattern{mustPattern(t, "{a}")},
						Permissions: ProjectPermissions{"repository": {"pull"}},
					}}},
				}
			},
		},
		{
			name: "unused identity",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x")}, {"a": mustPattern(t, "y")}},
					System: System{Grants: map[string]SystemGrant{"g": {
						When:        When{"a": "x"},
						Permissions: SystemPermissions{"project": {"create"}},
					}}},
				}
			},
			wantErr: "identities[1]: identity is not matched by any grant",
		},
		{
			name: "unused known claim",
			cfg: func(t *testing.T) Config {
				return Config{
					KnownClaims: []string{"a", "b"},
					Identities:  []Identity{{"a": mustPattern(t, "x")}},
					System: System{Grants: map[string]SystemGrant{"g": {
						Permissions: SystemPermissions{"project": {"create"}},
					}}},
				}
			},
			wantErr: `known_claims[1]: claim is not used by any identity or "when"`,
		},
		{
			name: "empty project pattern",
			cfg: func(t *testing.T) Config {
				return Config{
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						When:        When{"a": "x"},
						Projects:    []Pattern{mustPattern(t, "")},
						Permissions: ProjectPermissions{"repository": {"pull"}},
					}}},
				}
			},
			wantErr: "projects.grants.g.projects[0]: cannot be blank",
		},
		{
			name: "role pattern matches no role",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "{env}")}},
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						Projects: []Pattern{mustPattern(t, "p")},
						Roles:    []Pattern{mustPattern(t, "{env}-push")},
					}}},
				}
			},
			wantErr: `projects.grants.g.roles[0]: no role matches "{env}-push"`,
		},
		{
			name: "unused role",
			cfg: func(t *testing.T) Config {
				return Config{
					Projects: Projects{Roles: map[string]ProjectPermissions{"r": {"repository": {"pull"}}}},
				}
			},
			wantErr: "projects.roles.r: role is not used by any grant",
		},
		{
			name: "role used via placeholder pattern",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "{env}")}},
					Projects: Projects{
						Roles: map[string]ProjectPermissions{"dev-push": {"repository": {"push"}}},
						Grants: map[string]ProjectGrant{"g": {
							Projects: []Pattern{mustPattern(t, "p")},
							Roles:    []Pattern{mustPattern(t, "{env}-push")},
						}},
					},
				}
			},
		},
		{
			name: "grant without roles or permissions",
			cfg: func(t *testing.T) Config {
				return Config{
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						When:     When{"a": "x"},
						Projects: []Pattern{mustPattern(t, "p")},
					}}},
				}
			},
			wantErr: "projects.grants.g: must have roles or permissions",
		},
		{
			name: "empty known claims allows no claims",
			cfg: func(t *testing.T) Config {
				return Config{
					KnownClaims: []string{},
					Identities:  []Identity{{"a": mustPattern(t, "x")}},
				}
			},
			wantErr: "identities[0].a: claim not listed in known_claims",
		},
		{
			name: "grant ID used in two sections",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x")}},
					Projects: Projects{Grants: map[string]ProjectGrant{"g": {
						Projects:    []Pattern{mustPattern(t, "p")},
						Permissions: ProjectPermissions{"repository": {"pull"}},
					}}},
					System: System{Grants: map[string]SystemGrant{"g": {
						Permissions: SystemPermissions{"project": {"create"}},
					}}},
				}
			},
			wantErr: "grant ID appears in multiple sections",
		},
		{
			name:    "empty known claim",
			cfg:     func(t *testing.T) Config { return Config{KnownClaims: []string{""}} },
			wantErr: "cannot be blank",
		},
		{
			name:    "duplicate known claims",
			cfg:     func(t *testing.T) Config { return Config{KnownClaims: []string{"a", "a"}} },
			wantErr: "duplicate entry: a",
		},
		{
			name: "duplicate role in grant",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x")}},
					Projects: Projects{
						Roles: map[string]ProjectPermissions{"pull": {"repository": {"pull"}}},
						Grants: map[string]ProjectGrant{"g": {
							Projects: []Pattern{mustPattern(t, "p")},
							Roles:    []Pattern{mustPattern(t, "pull"), mustPattern(t, "pull")},
						}},
					},
				}
			},
			wantErr: "duplicate entry: pull",
		},
		{
			name: "placeholder in permission",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x")}},
					AllProjects: AllProjects{Grants: map[string]AllProjectGrant{"g": {
						Permissions: ProjectPermissions{"repo{x}": {"pull"}},
					}}},
				}
			},
			wantErr: "placeholders like {name} are not supported here",
		},
		{
			name:    "placeholder in known claim",
			cfg:     func(t *testing.T) Config { return Config{KnownClaims: []string{"{a}"}} },
			wantErr: "placeholders like {name} are not supported here",
		},
		{
			name: "duplicate identities",
			cfg: func(t *testing.T) Config {
				id := Identity{"a": mustPattern(t, "x")}
				return Config{Identities: []Identity{id, id}}
			},
			wantErr: "duplicate entry: {a: x}",
		},
		{
			name: "placeholder in when",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"a": mustPattern(t, "x-{repo}")}},
					System: System{Grants: map[string]SystemGrant{"g": {
						When:        When{"a": "x-{repo}"},
						Permissions: SystemPermissions{"project": {"create"}},
					}}},
				}
			},
			wantErr: "placeholders like {name} are not supported here",
		},
		{
			name: "when matches no identity",
			cfg: func(t *testing.T) Config {
				return Config{
					Identities: []Identity{{"repository": mustPattern(t, "acme/{repo}")}},
					System: System{Grants: map[string]SystemGrant{"g": {
						When:        When{"repository": "contoso/x"},
						Permissions: SystemPermissions{"project": {"create"}},
					}}},
				}
			},
			wantErr: "no identity can match this condition",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg(t)
			err := cfg.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %s", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("wrong error\nwant: %q\ngot:  %v", tc.wantErr, err)
			}
		})
	}
}

func TestGrantPlaceholderNames(t *testing.T) {
	p := func(s string) []Pattern { return []Pattern{mustPattern(t, s)} }
	tests := []struct {
		name  string
		names map[string]bool
		want  []string
	}{
		{"project grant", ProjectGrant{Projects: p("{a}-x"), Roles: p("{b}")}.PlaceholderNames(), []string{"a", "b"}},
		{"all projects grant", AllProjectGrant{Roles: p("{c}")}.PlaceholderNames(), []string{"c"}},
		{"system grant", SystemGrant{Roles: p("{d}-{e}")}.PlaceholderNames(), []string{"d", "e"}},
		{"no placeholders", SystemGrant{Roles: p("plain")}.PlaceholderNames(), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Sorted(maps.Keys(tc.names))
			if !slices.Equal(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}
