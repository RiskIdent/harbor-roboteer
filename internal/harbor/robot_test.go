package harbor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
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

func TestNewRobotCreateRequest(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		metadata    RobotMetadata
		duration    int64
		description string
	}{
		{name: "zero expiry", duration: 1, description: ""},
		{
			name: "rounded expiry",
			metadata: RobotMetadata{
				Description: "hello",
				EarlyExpiry: now.Add(25 * time.Hour),
				DeleteToken: "sha256:token",
				ManagedBy:   "roboteer",
			},
			duration: 2,
			description: "desc: hello\n" +
				"earlyExpiry: 2026-11-19T13:00:00Z\n" +
				"deleteToken: sha256:token\n" +
				"managedBy: roboteer",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := newRobotCreateRequest(RobotCreate{
				NameBase: "repo:acme/foo",
				ProjectPermissions: map[string]Permissions{
					"acme": {"repository": {"pull", "push"}},
				},
				RobotMetadata: test.metadata,
			}, now, "abcd1234")
			if got.Name != "repo-acme-foo-abcd1234" {
				t.Errorf("name = %q, want %q", got.Name, "repo-acme-foo-abcd1234")
			}
			if got.Description != test.description {
				t.Errorf("description = %q, want %q", got.Description, test.description)
			}
			if got.Duration != test.duration {
				t.Errorf("duration = %d, want %d", got.Duration, test.duration)
			}
			if got.Level != "system" {
				t.Errorf("level = %q, want system", got.Level)
			}
			if len(got.Permissions) != 1 {
				t.Fatalf("permissions = %d, want 1", len(got.Permissions))
			}
			permission := got.Permissions[0]
			if permission.Kind != "project" || permission.Namespace != "acme" {
				t.Errorf("permission = %q/%q, want project/acme", permission.Kind, permission.Namespace)
			}
			if len(permission.Access) != 2 {
				t.Errorf("permission access = %d, want 2", len(permission.Access))
			}
		})
	}
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
	c, err := New(mustParseURL(srv.URL), "admin", "secret")
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

	created, err := c.CreateRobot(t.Context(), RobotCreate{
		NameBase:    "repo:acme/foo",
		Description: "desc",
		ProjectPermissions: map[string]Permissions{
			"acme": {"repository": {"pull", "push"}},
		},
	})
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

	if _, err := c.CreateRobot(t.Context(), RobotCreate{
		NameBase:    "repo:acme/foo",
		Description: "desc",
		ProjectPermissions: map[string]Permissions{
			"acme": {"repository": {"pull", "push"}},
		},
	}); !errors.Is(err, ErrRobotExists) {
		t.Fatalf("err = %v, want ErrRobotExists", err)
	}
	if calls != createAttempts {
		t.Fatalf("got %d create calls, want %d", calls, createAttempts)
	}
}
