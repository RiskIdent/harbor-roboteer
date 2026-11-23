package config

import (
	"reflect"
	"sync"
	"testing"
)

func TestEvaluate(t *testing.T) {
	t.Parallel()
	cfg := mustParse(t, `
		local lib = {
		  project(name, access):: { kind: 'project', namespace: name, access: access },
		  allProjects(access):: self.project('*', access),
		  system(access):: { kind: 'system', access: access },
		};
		function(claims) [
		  lib.project(claims.repo, { repository: ['push', 'pull'] }),
		  lib.project(claims.repo, { repository: ['pull'], artifact: ['read'] }),
		  if claims.repo == 'other' then lib.system({ project: ['create'] }),
		  lib.allProjects({ repository: ['pull'] }),
		  lib.system({ label: ['create'] }),
		  { kind: 'system', namespace: '/', access: { label: ['update'] } },
		]
	`)
	got, err := cfg.Evaluate(map[string]any{"repo": "web"})
	if err != nil {
		t.Fatal(err)
	}
	want := Permissions{
		Projects:    map[string]Access{"web": {"repository": {"pull", "push"}, "artifact": {"read"}}},
		AllProjects: Access{"repository": {"pull"}},
		System:      Access{"label": {"create", "update"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wrong permissions\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestEvaluateConcurrently(t *testing.T) {
	t.Parallel()
	cfg := mustParse(t, `
		local lib = {
		  project(name, access):: { kind: 'project', namespace: name, access: access },
		  allProjects(access):: self.project('*', access),
		  system(access):: { kind: 'system', access: access },
		};
		function(claims) [lib.project(claims.repo, { repository: ['pull'] })]
	`)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := cfg.Evaluate(map[string]any{"repo": "web"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestEvaluateErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "not a list", source: `function(claims) { kind: 'system' }`},
		{name: "unknown field", source: `function(claims) [{ kind: 'system', access: { a: ['b'] }, extra: 1 }]`},
		{name: "unknown kind", source: `function(claims) [{ kind: 'robot', access: { a: ['b'] } }]`},
		{name: "project without namespace", source: `function(claims) [{ kind: 'project', access: { a: ['b'] } }]`},
		{name: "system with namespace", source: `function(claims) [{ kind: 'system', namespace: 'x', access: { a: ['b'] } }]`},
		{name: "empty access", source: `function(claims) [{ kind: 'system', access: {} }]`},
		{name: "empty actions", source: `function(claims) [{ kind: 'system', access: { a: [] } }]`},
		{name: "null actions", source: `function(claims) [{ kind: 'system', access: { a: null } }]`},
		{name: "empty action", source: `function(claims) [{ kind: 'system', access: { a: [''] } }]`},
		{name: "empty resource", source: `function(claims) [{ kind: 'system', access: { '': ['b'] } }]`},
		{name: "missing claim", source: `function(claims) [claims.missing]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := mustParse(t, test.source).Evaluate(map[string]any{}); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestParseRejectsSyntaxError(t *testing.T) {
	t.Parallel()
	if _, err := Parse("test.jsonnet", []byte(`function(claims) [`)); err == nil {
		t.Fatal("want error, got nil")
	}
}

func mustParse(t *testing.T, source string) *Config {
	t.Helper()
	cfg, err := Parse("test.jsonnet", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
