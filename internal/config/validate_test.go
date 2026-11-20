package config

import (
	"testing"
)

func TestFlowYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"identity", Identity{"b": MustPattern("{repo}"), "a": MustPattern("acme/x")}, "{a: acme/x, b: '{repo}'}"},
		{"empty map", Identity{}, "{}"},
		{"nil map", Identity(nil), "{}"},
		{"string", "hello", "hello"},
		{"list", []string{"a", "b"}, "[a, b]"},
		{"nested", map[string][]string{"repository": {"list", "pull"}}, "{repository: [list, pull]}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := flowYAML(tc.value)
			if got != tc.want {
				t.Errorf("wrong output\nwant: %s\ngot:  %s", tc.want, got)
			}
		})
	}
}

func TestConfigValidate_flatErrors(t *testing.T) {
	t.Parallel()
	cfg := Config{
		KnownClaims: []string{"repository", "repository"},
		Identities:  []Identity{{"foo": MustPattern("bar")}},
	}
	want := "identities[0].foo: claim not listed in known_claims\nknown_claims[1]: duplicate entry: repository"
	err := cfg.Validate()
	if err == nil || err.Error() != want {
		t.Errorf("wrong error\nwant: %q\ngot:  %v", want, err)
	}
}

func TestJoinPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path, key, want string
	}{
		{"", "foo", "foo"},
		{"foo", "bar", "foo.bar"},
		{"foo", "0", "foo[0]"},
		{"foo[0]", "bar", "foo[0].bar"},
		{"foo", "1a", "foo.1a"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := joinPath(tc.path, tc.key); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}
