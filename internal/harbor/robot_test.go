package harbor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// harborNameRe is Harbor's own robot name check (https://github.com/goharbor/harbor/blob/8499cade41eafb3cd1de49fac2bc2c240ba758c7/src/server/v2.0/handler/robot.go#L492).
var harborNameRe = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

func TestNormalizeRobotName(t *testing.T) {
	const suffix = "abcd1234"
	tests := []struct{ base, want string }{
		{"repo:acme/foo:environment:harbor", "repo-acme-foo-environment-harbor-" + suffix},
		{"Repo:ACME/Foo", "repo-acme-foo-" + suffix},
		{"::weird//..chars--", "weird-chars-" + suffix},
		{"ünïcode sub", "n-code-sub-" + suffix},
		{"", suffix},
		{":::", suffix},
		{strings.Repeat("a", 300), strings.Repeat("a", maxNameLen-len(suffix)-1) + "-" + suffix},
		{strings.Repeat("a", maxNameLen-10) + ":b", strings.Repeat("a", maxNameLen-10) + "-" + suffix},
	}
	for _, tt := range tests {
		got := normalizeRobotName(tt.base, suffix)
		if got != tt.want {
			t.Errorf("normalizeRobotName(%q) = %q, want %q", tt.base, got, tt.want)
		}
		if !harborNameRe.MatchString(got) {
			t.Errorf("normalizeRobotName(%q) = %q, rejected by Harbor's name check", tt.base, got)
		}
		if len(got) > maxNameLen {
			t.Errorf("normalizeRobotName(%q) is %d chars, want <= %d", tt.base, len(got), maxNameLen)
		}
	}
}

// FuzzNormalizeRobotName checks that any input yields a name Harbor accepts.
// Run longer with: go test ./internal/harbor -run '^$' -fuzz FuzzNormalizeRobotName -fuzztime 2h
func FuzzNormalizeRobotName(f *testing.F) {
	for _, seed := range []string{
		"repo:acme/foo:environment:harbor", "Repo:ACME/Foo", "::weird//..chars--", "ünïcode sub", "", ":::",
		strings.Repeat("a", 300), strings.Repeat("a", maxNameLen-10) + ":b",
	} {
		f.Add(seed)
	}
	const suffix = "abcd1234"
	f.Fuzz(func(t *testing.T, base string) {
		got := normalizeRobotName(base, suffix)
		if !harborNameRe.MatchString(got) {
			t.Fatalf("normalizeRobotName(%q) = %q, rejected by Harbor's name check", base, got)
		}
		if len(got) > maxNameLen {
			t.Fatalf("normalizeRobotName(%q) is %d chars, want <= %d", base, len(got), maxNameLen)
		}
		if got != suffix && !strings.HasSuffix(got, "-"+suffix) {
			t.Fatalf("normalizeRobotName(%q) = %q, want it to end in the suffix", base, got)
		}
	})
}

// fakeHarbor serves Harbor's API, passing each requested robot name to respond, which writes the response.
func fakeHarbor(t *testing.T, respond func(w http.ResponseWriter, name string)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		respond(w, body.Name)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateRobotRetriesOnConflict(t *testing.T) {
	var names []string
	c := fakeHarbor(t, func(w http.ResponseWriter, name string) {
		names = append(names, name)
		if len(names) < 3 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "robot$" + name, "secret": "s"})
	})

	created, err := c.CreateRobot(t.Context(), "repo:acme/foo", "acme", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("got %d create calls, want 3: %v", len(names), names)
	}
	if names[0] == names[1] || names[1] == names[2] {
		t.Errorf("retries reused a name: %v", names)
	}
	for _, n := range names {
		if !harborNameRe.MatchString(n) {
			t.Errorf("sent %q, rejected by Harbor's name check", n)
		}
	}
	if created.Name != "robot$"+names[2] {
		t.Errorf("created %q, want the last attempted name", created.Name)
	}
}

func TestCreateRobotGivesUpAfterAttempts(t *testing.T) {
	calls := 0
	c := fakeHarbor(t, func(w http.ResponseWriter, _ string) {
		calls++
		w.WriteHeader(http.StatusConflict)
	})

	if _, err := c.CreateRobot(t.Context(), "repo:acme/foo", "acme", "desc"); !errors.Is(err, ErrRobotExists) {
		t.Fatalf("err = %v, want ErrRobotExists", err)
	}
	if calls != createAttempts {
		t.Fatalf("got %d create calls, want %d", calls, createAttempts)
	}
}
