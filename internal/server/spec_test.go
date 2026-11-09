package server

import (
	"strings"
	"testing"
)

func TestSetSpecVersion(t *testing.T) {
	const marked = "version: 0.0.0 " + versionMarker

	tests := []struct {
		name       string
		spec       string
		appVersion string
		want       string // the whole spec, if no error
		wantErr    string
	}{
		{
			name:       "replaces the marked value, keeping the marker",
			spec:       "info:\n  " + marked + "\n",
			appVersion: "v1.2.3",
			want:       "info:\n  version: \"v1.2.3\" " + versionMarker + "\n",
		},
		{
			name:       "any indentation",
			spec:       "info:\n        " + marked + "\n",
			appVersion: "v1",
			want:       "info:\n        version: \"v1\" " + versionMarker + "\n",
		},
		{
			name:       "leaves other version lines alone, wherever they are",
			spec:       "externalDocs:\n  version: before\ninfo:\n  " + marked + "\ncomponents:\n  version: after\n  note: version: x\n",
			appVersion: "v2",
			want:       "externalDocs:\n  version: before\ninfo:\n  version: \"v2\" " + versionMarker + "\ncomponents:\n  version: after\n  note: version: x\n",
		},
		{
			name:       "$ in the version is kept as is",
			spec:       "info:\n  " + marked + "\n",
			appVersion: "v1.2.3$1${2}",
			want:       "info:\n  version: \"v1.2.3$1${2}\" " + versionMarker + "\n",
		},
		{
			name:       "versions that look like numbers stay strings",
			spec:       "info:\n  " + marked + "\n",
			appVersion: "1.10",
			want:       "info:\n  version: \"1.10\" " + versionMarker + "\n",
		},
		{
			name:       "without a version, the spec is as is",
			spec:       "info:\n  " + marked + "\n",
			appVersion: "",
			want:       "info:\n  " + marked + "\n",
		},
		{
			name:       "no marker",
			spec:       "info:\n  version: 0.0.0\n",
			appVersion: "v1",
			wantErr:    "got 0",
		},
		{
			name:       "marker on a line that isn't the version",
			spec:       "info:\n  title: x " + versionMarker + "\n",
			appVersion: "v1",
			wantErr:    "got 0",
		},
		{
			name:       "two markers",
			spec:       "info:\n  " + marked + "\nother:\n  " + marked + "\n",
			appVersion: "v1",
			wantErr:    "got 2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := setSpecVersion([]byte(tc.spec), tc.appVersion)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
