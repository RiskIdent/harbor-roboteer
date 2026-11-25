package harbor

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/retry"
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
				Expiry:      now.Add(25 * time.Hour),
				DeleteToken: "sha256:token",
				ManagedBy:   "roboteer",
			},
			duration: 2,
			description: "desc: hello\n" +
				"expiry: 2026-11-19T13:00:00Z\n" +
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
	os.Clearenv() // Prevent proxy settings from redirecting local test traffic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		respond(w, body.Name)
	}))
	srv.Config.SetKeepAlivesEnabled(false) // idle connections would block synctest from advancing time
	t.Cleanup(srv.Close)
	c, err := New(mustParseURL(srv.URL), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateRobotRetriesOnServerError(t *testing.T) {
	calls := 0
	c := fakeHarbor(t, func(w http.ResponseWriter, name string) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "boom"}}})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "robot$" + name, "secret": "s"})
	})

	synctest.Test(t, func(t *testing.T) {
		if _, err := c.CreateRobot(t.Context(), RobotCreate{NameBase: "repo:acme/foo"}); err != nil {
			t.Fatal(err)
		}
	})
	if calls != 2 {
		t.Fatalf("got %d create calls, want 2", calls)
	}
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
	if calls != nameConflictRetry.MaxAttempts {
		t.Fatalf("got %d create calls, want %d", calls, nameConflictRetry.MaxAttempts)
	}
}

func TestCleanupRobots(t *testing.T) {
	os.Clearenv() // Prevent proxy settings from redirecting local test traffic.
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	desc := func(managedBy string, expiry time.Time) string {
		return RobotMetadata{ManagedBy: managedBy, Expiry: expiry}.String()
	}
	type robotJSON struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	// Fill page 1 with foreign robots so the interesting ones land on page 2.
	var robots []robotJSON
	for i := range 15 { // more than the default page size of 10
		robots = append(robots, robotJSON{ID: int64(1000 + i), Name: "foreign", Description: "plain text"})
	}
	robots = append(robots,
		robotJSON{ID: 1, Name: "expired", Description: desc("harbor-roboteer v1", now.Add(-time.Hour))},
		robotJSON{ID: 2, Name: "expired-fails", Description: desc("harbor-roboteer v1", now.Add(-time.Hour))},
		robotJSON{ID: 3, Name: "expired-gone", Description: desc("harbor-roboteer v1", now.Add(-time.Hour))},
		robotJSON{ID: 4, Name: "expired-after-fail", Description: desc("harbor-roboteer v1", now.Add(-time.Hour))},
		robotJSON{ID: 5, Name: "valid", Description: desc("harbor-roboteer v1", now.Add(time.Hour))},
		robotJSON{ID: 6, Name: "other-tool", Description: desc("someone-else", now.Add(-time.Hour))},
		robotJSON{ID: 7, Name: "lookalike-tool", Description: desc("harbor-roboteer-other-tool v1", now.Add(-time.Hour))},
		robotJSON{ID: 8, Name: "no-version", Description: desc("harbor-roboteer", now.Add(-time.Hour))},
		robotJSON{ID: 9, Name: "expired-flaky", Description: desc("harbor-roboteer v1", now.Add(-time.Hour))},
	)

	var deleted []string
	failingDeletes := 0
	flakyFailed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			page, size := 1, 10 // Harbor defaults
			_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
			_, _ = fmt.Sscan(r.URL.Query().Get("page_size"), &size)
			lo, hi := min((page-1)*size, len(robots)), min(page*size, len(robots))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(robots[lo:hi])
		case r.Method == http.MethodDelete:
			id := strings.TrimPrefix(r.URL.Path, "/api/v2.0/robots/")
			switch id {
			case "2":
				failingDeletes++
				w.WriteHeader(http.StatusInternalServerError)
			case "9":
				if !flakyFailed {
					flakyFailed = true
					w.WriteHeader(http.StatusNotFound) // like a reverse proxy that can't reach Harbor
					return
				}
				deleted = append(deleted, id)
			case "3":
				w.WriteHeader(http.StatusNotFound)
			default:
				deleted = append(deleted, id)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	}))
	srv.Config.SetKeepAlivesEnabled(false) // idle connections would block synctest from advancing time
	defer srv.Close()

	c, err := New(mustParseURL(srv.URL), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	synctest.Test(t, func(t *testing.T) {
		n, err = c.CleanupRobots(t.Context(), now)
	})
	if err == nil || !strings.Contains(err.Error(), "expired-fails") {
		t.Errorf("err = %v, want error mentioning expired-fails", err)
	}
	if failingDeletes != retry.DefaultBackoff.MaxAttempts {
		t.Errorf("tried deleting expired-fails %d times, want %d", failingDeletes, retry.DefaultBackoff.MaxAttempts)
	}
	if n != 5 {
		t.Errorf("deleted count = %d, want 5", n)
	}
	if got := strings.Join(deleted, ","); got != "1,4,8,9" {
		t.Errorf("deleted IDs = %s, want 1,4,8,9", got)
	}
}
