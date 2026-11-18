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
