package harbor

import (
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

type RobotCreate struct {
	NameBase    string
	Project     string
	Expiry      *time.Time
	Description DescriptionKV
}

type Robot struct {
	ID          int64
	Name        string
	Expiry      *time.Time
	Secret      string
	Description DescriptionKV
}

type DescriptionKV struct {
	Message string
	KV      http.Header
}

func (d DescriptionKV) String() string {
	var sb strings.Builder
	if d.Message != "" {
		sb.WriteString(d.Message)
	}
	if d.KV != nil {
		if d.Message != "" {
			sb.WriteByte('\n')
		}
		// http.Header does have a .Write func, but that one uses "\r\n" delimiters...
		for _, key := range slices.Sorted(maps.Keys(d.KV)) {
			for _, value := range d.KV[key] {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(key)
				sb.WriteString(": ")
				sb.WriteString(value)
			}
		}
	}
	return sb.String()
}

func ParseDescriptionKV(desc string) DescriptionKV {
	lines := slices.Collect(strings.Lines(strings.TrimRightFunc(desc, unicode.IsSpace)))

	for i, line := range lines {
		if i == 0 {
			lines[i] = strings.TrimSpace(line)
		} else {
			lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
		}
	}

	var kv http.Header
	for i, line := range slices.Backward(lines) {
		if line == "" {
			lines = lines[:i] // "pop" line
			break
		}
		key, value, ok := cutKeyValue(line)
		if !ok {
			break
		}
		lines = lines[:i] // "pop" line
		if kv == nil {
			kv = http.Header{}
		}
		kv.Add(key, value)
	}

	for key := range kv {
		// because we added them in reverse order
		slices.Reverse(kv[key])
	}

	return DescriptionKV{
		Message: strings.TrimRightFunc(strings.Join(lines, "\n"), unicode.IsSpace),
		KV:      kv,
	}
}

var validKeyRegexp = regexp.MustCompile(`^[a-zA-Z0-9\-]+(-[a-zA-Z0-9\-]+)*$`)

func cutKeyValue(s string) (string, string, bool) {
	key, value, ok := strings.Cut(s, ":")
	if !ok {
		return "", "", false
	}
	if value != "" && !strings.HasPrefix(value, " ") {
		return "", "", false
	}
	if !validKeyRegexp.MatchString(key) {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}
