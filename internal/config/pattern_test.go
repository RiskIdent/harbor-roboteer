package config

import (
	"errors"
	"reflect"
	"testing"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Pattern
	}{
		{
			name:  "empty",
			input: "",
			want:  Pattern(nil),
		},
		{
			name:  "only plain",
			input: "foo/bar:moo/doo",
			want:  Pattern{{"foo/bar:moo/doo", false}},
		},
		{
			name:  "only glob",
			input: "{foo}",
			want:  Pattern{{"foo", true}},
		},
		{
			name:  "escaped braces",
			input: "{{foo}} {{{bar}}}",
			want:  Pattern{{"{foo} {", false}, {"bar", true}, {"}", false}},
		},
		{
			name:  "trim glob name",
			input: "{ \t foo \n }",
			want:  Pattern{{"foo", true}},
		},
		{
			name:  "mix plain and glob",
			input: "hello-{world}!",
			want:  Pattern{{"hello-", false}, {"world", true}, {"!", false}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParsePattern(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(test.want, got) {
				t.Errorf("wrong result\nwant: %#v\ngot:  %#v", test.want, got)
			}
		})
	}
}

func TestParsePattern_Error(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{
			name:  "unexpected closing brace",
			input: "foo}bar",
			want:  ErrPatternUnexpectedClosingBrace,
		},
		{
			name:  "unexpected opening brace",
			input: "foo{bar{moo}",
			want:  ErrPatternUnexpectedOpeningBrace,
		},
		{
			name:  "missing brace",
			input: "foo{bar",
			want:  ErrPatternMissingClosingBrace,
		},
		{
			name:  "duplicate glob name",
			input: "{foo}-{foo}",
			want:  ErrPatternDuplicateGlobName,
		},
		{
			name:  "adjacent globs",
			input: "{foo}{bar}",
			want:  ErrPatternAdjacentGlobs,
		},
		{
			name:  "empty glob name",
			input: "foo{}bar",
			want:  ErrPatternEmptyGlobName,
		},
		{
			name:  "blank glob name",
			input: "{ }",
			want:  ErrPatternEmptyGlobName,
		},
		{
			name:  "invalid pattern name",
			input: "foo{bar!}",
			want:  ErrPatternGlobNameInvalidCharacters,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p, err := ParsePattern(test.input)
			if err == nil {
				t.Fatalf("expected error, got nil\nunexpected result: %#v", p)
			}
			if !errors.Is(err, test.want) {
				t.Errorf("wrong error\ninput: %q\nwant: %q\ngot:  %q", test.input, test.want, err)
			}
		})
	}
}

func TestPatternFormat(t *testing.T) {
	tests := []struct {
		name    string
		pattern Pattern
		values  map[string]string
		want    string
	}{
		{
			name:    "empty",
			pattern: Pattern{},
			values:  map[string]string{},
			want:    "",
		},
		{
			name:    "nil",
			pattern: nil,
			values:  nil,
			want:    "",
		},
		{
			name:    "only plain with nil values",
			pattern: Pattern{{"foo/bar:moo/doo", false}},
			values:  nil,
			want:    "foo/bar:moo/doo",
		},
		{
			name:    "only glob",
			pattern: Pattern{{"foo", true}},
			values:  map[string]string{"foo": "bar"},
			want:    "bar",
		},
		{
			name:    "mix plain and glob",
			pattern: Pattern{{"hello-", false}, {"world", true}, {"!", false}},
			values: map[string]string{
				"world": "doo",
			},
			want: "hello-doo!",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.pattern.Format(test.values)
			if err != nil {
				t.Fatal(err)
			}
			if test.want != got {
				t.Errorf("wrong result\npattern: %#v\nwant: %q\ngot:  %q", test.pattern, test.want, got)
			}
		})
	}
}

func TestPatternFormat_error(t *testing.T) {
	pattern := Pattern{{"foo", true}}
	s, err := pattern.Format(map[string]string{"bar": "unused"})
	if err == nil {
		t.Fatalf("expected error, got nil\nunexpected result: %q", s)
	}
}

func TestPatternMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern Pattern
		value   string
		want    map[string]string
		wantOK  bool
	}{
		{name: "empty matches", pattern: Pattern{{"", false}}, value: "", wantOK: true},
		{name: "nil matches", pattern: nil, value: "", wantOK: true},
		{name: "empty no match", pattern: Pattern{{"", false}}, value: "foo"},
		{name: "nil no match", pattern: nil, value: "foo"},
		{
			name:    "only plain matches",
			pattern: Pattern{{"foo/bar:moo/doo", false}},
			value:   "foo/bar:moo/doo",
			wantOK:  true,
		},
		{
			name:    "only plain prefix no match",
			pattern: Pattern{{"foo/bar:moo/doo", false}},
			value:   "foo/bar:moo/doo:hello",
		},
		{
			name:    "only glob matches",
			pattern: Pattern{{"foo", true}},
			value:   "bar",
			want:    map[string]string{"foo": "bar"},
			wantOK:  true,
		},
		{
			name:    "empty capture",
			pattern: Pattern{{"foo", true}},
			value:   "",
			want:    map[string]string{"foo": ""},
			wantOK:  true,
		},
		{
			name:    "only glob matches delimiters",
			pattern: Pattern{{"foo", true}},
			value:   "bar/moo:doo",
			want:    map[string]string{"foo": "bar/moo:doo"},
			wantOK:  true,
		},
		{
			name:    "explicit match across delimiters",
			pattern: Pattern{{"foo", true}, {"/", false}, {"baz", true}},
			value:   "bar/moo",
			want:    map[string]string{"foo": "bar", "baz": "moo"},
			wantOK:  true,
		},
		{
			name:    "glob stops at first following char",
			pattern: Pattern{{"a", true}, {"-", false}, {"b", true}},
			value:   "x-y-z",
			want:    map[string]string{"a": "x", "b": "y-z"},
			wantOK:  true,
		},
		{
			name:    "glob stops at entire following text",
			pattern: Pattern{{"a", true}, {"-x", false}, {"b", true}},
			value:   "foo-y-xbar",
			want:    map[string]string{"a": "foo-y", "b": "bar"},
			wantOK:  true,
		},
		{
			name:    "following plain text mismatch",
			pattern: Pattern{{"a", true}, {"-x", false}},
			value:   "foo-y",
		},
		{
			name:    "missing following char",
			pattern: Pattern{{"a", true}, {"-", false}, {"b", true}},
			value:   "xyz",
		},
		{
			name:    "plain prefix and suffix",
			pattern: Pattern{{"acme/", false}, {"repo", true}, {"!", false}},
			value:   "acme/foo!",
			want:    map[string]string{"repo": "foo"},
			wantOK:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := test.pattern.Match(test.value)
			if test.wantOK != ok || !reflect.DeepEqual(test.want, got) {
				t.Errorf("wrong result\npattern: %#v\nvalue: %q\nwant: %v, %t\ngot:  %v, %t", test.pattern, test.value, test.want, test.wantOK, got, ok)
			}
		})
	}
}

func TestPatternString(t *testing.T) {
	tests := []struct {
		name    string
		pattern Pattern
		want    string
	}{
		{name: "nil", pattern: nil, want: ""},
		{name: "empty", pattern: Pattern{}, want: ""},
		{name: "only plain", pattern: Pattern{{"foo/bar:moo/doo", false}}, want: "foo/bar:moo/doo"},
		{name: "only glob", pattern: Pattern{{"foo", true}}, want: "{foo}"},
		{
			name:    "mix plain and glob",
			pattern: Pattern{{"hello-", false}, {"world", true}, {"!", false}},
			want:    "hello-{world}!",
		},
		{
			name:    "multiple globs",
			pattern: Pattern{{"a", true}, {"-", false}, {"b", true}},
			want:    "{a}-{b}",
		},
		{
			name:    "escaped braces",
			pattern: Pattern{{"{foo} {", false}, {"bar", true}, {"}", false}},
			want:    "{{foo}} {{{bar}}}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.pattern.String()
			if test.want != got {
				t.Errorf("wrong result\npattern: %#v\nwant: %q\ngot:  %q", test.pattern, test.want, got)
			}
			if len(test.pattern) == 0 {
				return
			}
			parsed, err := ParsePattern(got)
			if err != nil {
				t.Fatalf("round-trip parse: %s", err)
			}
			if !reflect.DeepEqual(test.pattern, parsed) {
				t.Errorf("round-trip mismatch\nwant: %#v\ngot:  %#v", test.pattern, parsed)
			}
		})
	}
}

func TestPatternValidate_emptyPlainText(t *testing.T) {
	err := Pattern{{"", false}}.Validate()
	if !errors.Is(err, ErrPatternEmptyPlainText) {
		t.Errorf("wrong error\nwant: %q\ngot:  %q", ErrPatternEmptyPlainText, err)
	}
}

func TestPatternValidate_adjacentPlain(t *testing.T) {
	err := Pattern{{"foo", false}, {"bar", false}}.Validate()
	if !errors.Is(err, ErrPatternAdjacentPlain) {
		t.Errorf("wrong error\nwant: %q\ngot:  %q", ErrPatternAdjacentPlain, err)
	}
}
