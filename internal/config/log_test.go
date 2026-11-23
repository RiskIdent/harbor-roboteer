package config

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
)

func TestFlowYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"access", Access{"repository": {"pull", "list"}, "artifact": {"read"}}, "{artifact: [read], repository: [pull, list]}"},
		{"empty map", Access{}, "{}"},
		{"nil map", Access(nil), "{}"},
		{"string", "hello", "hello"},
		{"quoted string", "{repo}", "'{repo}'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := flowYAML(tc.value)
			if got != tc.want {
				t.Errorf("wrong output\nwant: %s\ngot:  %s", tc.want, got)
			}
		})
	}
}

func TestPermissionsLogValue(t *testing.T) {
	t.Parallel()
	permissions := Permissions{
		Projects:    map[string]Access{"web": {"repository": {"pull", "push"}, "artifact": {"read"}}},
		AllProjects: Access{"repository": {"pull"}},
	}
	withoutTime := &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	}
	tests := []struct {
		name       string
		newHandler func(w io.Writer) slog.Handler
		want       string
	}{
		{
			name:       "text",
			newHandler: func(w io.Writer) slog.Handler { return slog.NewTextHandler(w, withoutTime) },
			want:       `level=INFO msg=created permissions="{projects: {web: {artifact: [read], repository: [pull, push]}}, allProjects: {repository: [pull]}, system: {}}"` + "\n",
		},
		{
			name:       "json",
			newHandler: func(w io.Writer) slog.Handler { return slog.NewJSONHandler(w, withoutTime) },
			want:       `{"level":"INFO","msg":"created","permissions":{"projects":{"web":{"artifact":["read"],"repository":["pull","push"]}},"allProjects":{"repository":["pull"]},"system":{}}}` + "\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			slog.New(test.newHandler(&buf)).InfoContext(t.Context(), "created", slog.Any("permissions", permissions))
			if got := buf.String(); got != test.want {
				t.Errorf("wrong log output\nwant: %s\ngot:  %s", test.want, got)
			}
		})
	}
}
