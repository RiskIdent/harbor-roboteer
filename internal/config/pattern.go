package config

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"strconv"
	"strings"
)

// Pattern is plain text mixed with {capture} elements, such as "hello-{world}!".
type Pattern []PatternElem

var (
	ErrPatternUnexpectedClosingBrace       = errors.New("unexpected closing '}' before opening '{'")
	ErrPatternMissingClosingBrace          = errors.New("missing closing '}' after opening '{'")
	ErrPatternUnexpectedOpeningBrace       = errors.New("expected closing '}' after opening '{', but got another opening '{'")
	ErrPatternDuplicateCaptureName         = errors.New("pattern capture name used more than once")
	ErrPatternAdjacentCaptures             = errors.New("pattern captures must be separated by plain text")
	ErrPatternAdjacentPlain                = errors.New("plain text elements must be separated by a capture")
	ErrPatternEmptyCaptureName             = errors.New("pattern capture name is empty")
	ErrPatternEmptyPlainText               = errors.New("plain text element is empty")
	ErrPatternCaptureNameInvalidCharacters = errors.New("pattern capture name contains invalid characters. Must match: ^[a-zA-Z0-9._-]+$")
)

var patternCaptureNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

var patternPlainEscaper = strings.NewReplacer(
	"{", "{{",
	"}", "}}",
)

var (
	_ encoding.TextAppender    = Pattern{}
	_ encoding.TextMarshaler   = Pattern{}
	_ encoding.TextUnmarshaler = &Pattern{}
)

// ParsePattern parses a pattern such as "hello-{world}!".
// Use "{{" and "}}" to write literal braces.
func ParsePattern(s string) (Pattern, error) {
	parser := patternParser{}
	if err := parser.parse(s); err != nil {
		return nil, err
	}
	if len(parser.pattern) == 0 {
		return nil, nil
	}
	if err := parser.pattern.Validate(); err != nil {
		return nil, err
	}
	return parser.pattern, nil
}

type patternParser struct {
	pattern Pattern
	plain   strings.Builder
}

func (p *patternParser) parse(s string) error {
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "{{"):
			p.plain.WriteByte('{')
			i += 2
		case strings.HasPrefix(s[i:], "}}"):
			p.plain.WriteByte('}')
			i += 2
		case s[i] == '}':
			return ErrPatternUnexpectedClosingBrace
		case s[i] == '{':
			end := strings.IndexByte(s[i:], '}')
			if end == -1 {
				return ErrPatternMissingClosingBrace
			}
			name := strings.TrimSpace(s[i+1 : i+end])
			if strings.Contains(name, "{") {
				return ErrPatternUnexpectedOpeningBrace
			}
			p.flushPlain()
			p.pattern = append(p.pattern, PatternElem{Text: name, IsCapture: true})
			i += end + 1 // +1 to skip the closing '}'
		default:
			p.plain.WriteByte(s[i])
			i++
		}
	}
	p.flushPlain()
	return nil
}

func (p *patternParser) flushPlain() {
	if p.plain.Len() == 0 {
		return
	}
	p.pattern = append(p.pattern, PatternElem{Text: p.plain.String()})
	p.plain.Reset()
}

// Validate checks that plain text elements are non-empty, capture names are
// valid and unique, and that plain text and captures alternate.
func (p Pattern) Validate() error {
	for i, elem := range p {
		if i > 0 && p[i-1].IsCapture == elem.IsCapture {
			// Cannot happen via ParsePattern, but can via manually constructed Pattern.
			// Reject these as they don't survive String->ParsePattern roundtrips
			if elem.IsCapture {
				return ErrPatternAdjacentCaptures
			}
			return ErrPatternAdjacentPlain
		}
		if !elem.IsCapture {
			if elem.Text == "" {
				// Cannot happen via ParsePattern, but can via manually constructed Pattern.
				// Reject these as they mess up Match logic
				return ErrPatternEmptyPlainText
			}
			continue
		}
		if elem.Text == "" {
			return ErrPatternEmptyCaptureName
		}
		if !patternCaptureNameRegexp.MatchString(elem.Text) {
			return ErrPatternCaptureNameInvalidCharacters
		}
		if p[:i].HasCaptureName(elem.Text) {
			return fmt.Errorf("%w: %q", ErrPatternDuplicateCaptureName, elem.Text)
		}
	}
	return nil
}

func (p Pattern) CaptureCount() int {
	var count int
	for _, elem := range p {
		if elem.IsCapture {
			count++
		}
	}
	return count
}

func (p Pattern) HasCaptureName(name string) bool {
	for captureName := range p.CaptureNames() {
		if captureName == name {
			return true
		}
	}
	return false
}

func (p Pattern) CaptureNames() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, elem := range p {
			if elem.IsCapture {
				if !yield(elem.Text) {
					return
				}
			}
		}
	}
}

func (p Pattern) Format(values map[string]string) (string, error) {
	if len(p) == 0 {
		return "", nil
	}
	if len(p) == 1 && !p[0].IsCapture {
		// zero-alloc path
		return p[0].Text, nil
	}
	size := 0
	for _, elem := range p {
		if !elem.IsCapture {
			size += len(elem.Text)
			continue
		}
		val, ok := values[elem.Text]
		if !ok {
			// don't need to use %q, as capture names are validated during parsing
			return "", fmt.Errorf("pattern requires \"{%s}\", but no such value was provided", elem.Text)
		}
		size += len(val)
	}
	if len(p) == 1 {
		// p[0] is known to be capture here, thanks to previous if-statement
		// zero-alloc path
		return values[p[0].Text], nil
	}
	var sb strings.Builder
	sb.Grow(size)
	for _, elem := range p {
		if !elem.IsCapture {
			sb.WriteString(elem.Text)
		} else {
			sb.WriteString(values[elem.Text])
		}
	}
	return sb.String(), nil
}

// Match reports whether s matches the pattern, and returns the value
// captured by each capture.
//
// A capture captures any characters up to the first occurrence of the entire plain text that follows it,
// or to the end of the string if it is the last element.
//
// Match doesn't backtrack, so it isn't the inverse of [Pattern.Format]:
// "{env}-pull" formats {env: "a-pull"} as "a-pull-pull", but doesn't match it.
// Use [Pattern.CanFormat] to check what a pattern can produce.
func (p Pattern) Match(s string) (map[string]string, bool) {
	var values map[string]string
	rest := s
	for i, elem := range p {
		if !elem.IsCapture {
			var ok bool
			rest, ok = strings.CutPrefix(rest, elem.Text)
			if !ok {
				return nil, false
			}
			continue
		}
		end := len(rest)
		if i+1 < len(p) && p[i+1].Text != "" {
			// if followed by plain text, then match until first char of next text
			end = strings.Index(rest, p[i+1].Text)
			if end == -1 {
				// pattern: "{foo}-x-{bar}"
				// but rest does not contain "-x-"
				return nil, false
			}
		}
		val := rest[:end]
		if values == nil {
			values = make(map[string]string, p.CaptureCount())
		}
		values[elem.Text] = val
		rest = rest[end:]
	}
	if rest != "" {
		return nil, false
	}
	return values, true
}

// CanFormat reports whether some values for the captures make
// [Pattern.Format] produce s. Use it when the pattern is a template to fill in,
// and not, like [Pattern.Match], a pattern to extract values with.
//
// It can give false positives, as it doesn't know which values are possible.
//
// Implemented by constructing a dynamic regexp program.
// Not suitable for hot paths.
func (p Pattern) CanFormat(s string) bool {
	var expr strings.Builder
	expr.WriteString("(?s)^") // (?s) makes "." match newlines too
	for _, elem := range p {
		if elem.IsCapture {
			expr.WriteString(".*")
		} else {
			expr.WriteString(regexp.QuoteMeta(elem.Text))
		}
	}
	expr.WriteString("$")
	return regexp.MustCompile(expr.String()).MatchString(s)
}

// String implements [fmt.Stringer]
func (p Pattern) String() string {
	if len(p) == 0 {
		return ""
	}
	if len(p) == 1 && !p[0].IsCapture && !strings.ContainsAny(p[0].Text, "{}") {
		// zero-alloc path
		return p[0].Text
	}
	size := 0
	for _, elem := range p {
		size += len(elem.Text)
		if elem.IsCapture {
			size += 2 // for the braces
		}
	}
	b := make([]byte, 0, size)
	b, _ = p.AppendText(b)
	return string(b)
}

// AppendText implements [encoding.TextAppender].
func (p Pattern) AppendText(b []byte) ([]byte, error) {
	buf := bytes.NewBuffer(b)
	for _, elem := range p {
		if !elem.IsCapture {
			_, _ = patternPlainEscaper.WriteString(buf, elem.Text) // bytes.Buffer never errors
		} else {
			buf.WriteByte('{')
			buf.WriteString(elem.Text)
			buf.WriteByte('}')
		}
	}
	return buf.Bytes(), nil
}

// MarshalText implements [encoding.TextMarshaler].
func (p Pattern) MarshalText() (text []byte, err error) {
	return p.AppendText(nil)
}

// UnmarshalText implements [encoding.TextUnmarshaler].
func (p *Pattern) UnmarshalText(text []byte) error {
	return p.Set(string(text))
}

// Set implements [github.com/spf13/pflag.Value].
func (p *Pattern) Set(s string) error {
	newPattern, err := ParsePattern(s)
	if err != nil {
		return err
	}
	*p = newPattern
	return nil
}

// Type implements [github.com/spf13/pflag.Value].
func (p Pattern) Type() string {
	return "pattern"
}

type PatternElem struct {
	Text      string
	IsCapture bool
}

// String implements [fmt.GoStringer]
func (elem PatternElem) GoString() string {
	if elem.IsCapture {
		return fmt.Sprintf("{%s}", elem.Text)
	}
	return strconv.Quote(elem.Text)
}
