package harbor

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

func TestPermissionsHarborAccess(t *testing.T) {
	got := (Permissions{
		"repository": {"pull", "push"},
		"artifact":   {"delete"},
	}).HarborAccess()

	access := make([]string, 0, len(got))
	for _, item := range got {
		access = append(access, item.Resource+":"+item.Action)
	}
	slices.Sort(access)
	want := []string{"artifact:delete", "repository:pull", "repository:push"}
	if !slices.Equal(access, want) {
		t.Errorf("wrong access\nwant: %v\ngot:  %v", want, access)
	}
}

func TestRobotPermissions(t *testing.T) {
	got := (RobotCreate{
		SystemPermissions: Permissions{"project": {"create"}},
		AllProjectPermissions: Permissions{
			"repository": {"list"},
		},
		ProjectPermissions: map[string]Permissions{
			"zeta":  {"repository": {"push"}},
			"alpha": {"repository": {"pull"}},
		},
	}).RobotPermissions()

	if len(got) != 4 {
		t.Fatalf("permissions = %d, want 4: %#v", len(got), got)
	}
	want := []struct {
		kind      string
		namespace string
		resource  string
		action    string
	}{
		{kind: "system", namespace: "/", resource: "project", action: "create"},
		{kind: "project", namespace: "*", resource: "repository", action: "list"},
		{kind: "project", namespace: "alpha", resource: "repository", action: "pull"},
		{kind: "project", namespace: "zeta", resource: "repository", action: "push"},
	}
	for i, permission := range got {
		if permission.Kind != want[i].kind {
			t.Errorf("permission %d kind = %q, want %q", i, permission.Kind, want[i].kind)
		}
		if permission.Namespace != want[i].namespace {
			t.Errorf("permission %d namespace = %q, want %q", i, permission.Namespace, want[i].namespace)
		}
		if len(permission.Access) != 1 {
			t.Errorf("permission %d access = %d, want 1", i, len(permission.Access))
			continue
		}
		if permission.Access[0].Resource != want[i].resource || permission.Access[0].Action != want[i].action {
			t.Errorf("permission %d access = %s:%s, want %s:%s",
				i,
				permission.Access[0].Resource,
				permission.Access[0].Action,
				want[i].resource,
				want[i].action,
			)
		}
	}
}

func TestRobotPermissionsEmpty(t *testing.T) {
	if got := (RobotCreate{}).RobotPermissions(); len(got) != 0 {
		t.Errorf("permissions = %#v, want none", got)
	}
}

func TestRobotMetadataString(t *testing.T) {
	tests := []struct {
		name  string
		input RobotMetadata
		want  string
	}{
		{name: "empty", input: RobotMetadata{}, want: ""},
		{name: "description", input: RobotMetadata{Description: "hello"}, want: "desc: hello"},
		{name: "delete token", input: RobotMetadata{DeleteToken: "foobar"}, want: "deleteToken: foobar"},
		{
			name: "full",
			input: RobotMetadata{
				Description: "hello",
				Expiry:      time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC),
				DeleteToken: "heyomayo",
				ManagedBy:   "harbor-roboteer",
			},
			want: "desc: hello\n" +
				"earlyExpiry: 2026-11-18T12:00:00Z\n" +
				"deleteToken: heyomayo\n" +
				"managedBy: harbor-roboteer",
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

func TestParseRobotMetadata(t *testing.T) {
	creation := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	early := creation.Add(time.Hour)
	tests := []struct {
		name     string
		input    string
		duration *int64
		want     RobotMetadata
	}{
		{name: "empty", input: "", want: RobotMetadata{}},
		{name: "plain description", input: "hello world!", want: RobotMetadata{Description: "hello world!"}},
		{name: "malformed yaml", input: "desc: [", want: RobotMetadata{Description: "desc: ["}},
		{
			name:  "yaml metadata",
			input: "desc: hello\nearlyExpiry: 2026-11-18T13:00:00Z\ndeleteToken: token\nmanagedBy: roboteer\n",
			want:  RobotMetadata{Description: "hello", Expiry: early, DeleteToken: "token", ManagedBy: "roboteer"},
		},
		{
			name:     "early expiry overrides duration",
			input:    "earlyExpiry: 2026-11-18T13:00:00Z\n",
			duration: new(int64(2)),
			want:     RobotMetadata{Expiry: early},
		},
		{
			name:     "duration fallback",
			input:    "desc: hello\n",
			duration: new(int64(2)),
			want:     RobotMetadata{Description: "hello", Expiry: creation.Add(48 * time.Hour)},
		},
		{name: "never expires", input: "desc: hello\n", duration: new(int64(-1)), want: RobotMetadata{Description: "hello"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseRobotMetadata(test.input, strfmt.DateTime(creation), test.duration)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("wrong result\nwant: %#v\ngot:  %#v", test.want, got)
			}
		})
	}
}

func TestRobotMetadataRoundTrip(t *testing.T) {
	input := RobotMetadata{
		Description: "hello: world\nsecond line",
		Expiry:      time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC),
		DeleteToken: "sha256:0123",
		ManagedBy:   "harbor-roboteer",
	}
	got := ParseRobotMetadata(input.String(), strfmt.DateTime(time.Time{}), nil)
	if !reflect.DeepEqual(got, input) {
		t.Errorf("wrong result\nwant: %#v\ngot:  %#v", input, got)
	}
}

func TestNewRobotFromResponse(t *testing.T) {
	creation := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	got := NewRobotFromResponse(&models.Robot{
		ID:           42,
		Name:         "robot$example",
		Secret:       "secret",
		CreationTime: strfmt.DateTime(creation),
		Description:  "desc: hello\ndeleteToken: token\n",
		Duration:     new(int64(2)),
	})
	want := Robot{
		ID:          42,
		Name:        "robot$example",
		Secret:      "secret",
		Description: "hello",
		Expiry:      creation.Add(48 * time.Hour),
		DeleteToken: "token",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wrong result\nwant: %#v\ngot:  %#v", want, got)
	}
}
