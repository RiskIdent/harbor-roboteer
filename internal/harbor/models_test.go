package harbor

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestDescriptionKVString(t *testing.T) {
	tests := []struct {
		name  string
		input DescriptionKV
		want  string
	}{
		{name: "empty", input: DescriptionKV{}, want: ""},
		{
			name:  "only message",
			input: DescriptionKV{Message: "hello"},
			want:  "hello",
		},
		{
			name:  "only kv",
			input: DescriptionKV{KV: http.Header{"Key": {"value"}}},
			want:  "Key: value",
		},
		{
			name:  "message and kv",
			input: DescriptionKV{Message: "hello", KV: http.Header{"Key": {"value1", "value2"}}},
			want:  "hello\n\nKey: value1\nKey: value2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.input.String(); got != test.want {
				t.Errorf("wrong result\nwant: %q\ngot:  %q", test.want, got)
			}
		})
	}
}

func TestParseDescriptionKV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  DescriptionKV
	}{
		{name: "empty", input: "", want: DescriptionKV{}},
		{name: "only whitespace", input: "   \n  \t\n\n  ", want: DescriptionKV{}},
		{name: "only message", input: "hello world!", want: DescriptionKV{Message: "hello world!"}},
		{name: "multiline message", input: "hello\nworld\n!", want: DescriptionKV{Message: "hello\nworld\n!"}},
		{name: "trim whitespace", input: "  hello\n  world!  \n\n  ", want: DescriptionKV{Message: "hello\n  world!"}},
		{name: "only kv", input: "some-key: some-value", want: DescriptionKV{KV: http.Header{
			"Some-Key": {"some-value"},
		}}},
		{name: "trailing blank lines after kv", input: "hello\n\nkey: value\n\n  ", want: DescriptionKV{
			Message: "hello",
			KV:      http.Header{"Key": {"value"}},
		}},
		{name: "crlf and case-insensitive duplicates", input: "hello\r\n\r\nkey: first\r\nKEY: second: more\r\n", want: DescriptionKV{
			Message: "hello",
			KV:      http.Header{"Key": {"first", "second: more"}},
		}},
		{name: "invalid key stops metadata", input: "hello\ninvalid key: value\nkey: value", want: DescriptionKV{
			Message: "hello\ninvalid key: value",
			KV:      http.Header{"Key": {"value"}},
		}},
		{name: "indented line stops metadata", input: "hello\n key: value\nkey: value", want: DescriptionKV{
			Message: "hello\n key: value",
			KV:      http.Header{"Key": {"value"}},
		}},
		{name: "valid mix", input: "hello world!\n\nsome-key: some-value\nother-key: other-value1\nother-key: other-value2", want: DescriptionKV{
			Message: "hello world!",
			KV: http.Header{
				"Some-Key":  {"some-value"},
				"Other-Key": {"other-value1", "other-value2"},
			},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseDescriptionKV(test.input)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("wrong result\nwant: %#v\ngot:  %#v", test.want, got)
			}
		})
	}
}

func FuzzParseDescriptionKV(f *testing.F) {
	for _, seed := range []string{"", "hello", "hello\n\nkey: value", "key: first\nKEY: second", "  \nkey: value\n\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := ParseDescriptionKV(input)
		crlf := ParseDescriptionKV(strings.ReplaceAll(input, "\n", "\r\n"))
		if !reflect.DeepEqual(got, crlf) {
			t.Errorf("LF and CRLF results differ for %q: %#v != %#v", input, got, crlf)
		}
		trailingWhitespace := ParseDescriptionKV(input + "\n\n \t")
		if !reflect.DeepEqual(got, trailingWhitespace) {
			t.Errorf("trailing whitespace changes result for %q: %#v != %#v", input, got, trailingWhitespace)
		}
	})
}
