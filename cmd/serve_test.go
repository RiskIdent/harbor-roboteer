package cmd

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestLoadServeOptionsRobotTTL(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want time.Duration
	}{
		{
			name: "environment",
			env:  "90m",
			want: 90 * time.Minute,
		},
		{
			name: "flag overrides environment",
			env:  "90m",
			args: []string{"--robot-ttl=2h"},
			want: 2 * time.Hour,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ROBOTEER_ROBOT_TTL", test.env)
			flags := pflag.NewFlagSet("serve", pflag.ContinueOnError)
			flags.Duration("robot-ttl", ServeDefaults.RobotTTL, "")
			if err := flags.Parse(test.args); err != nil {
				t.Fatal(err)
			}

			options, err := LoadServeOptions(flags)
			if err != nil {
				t.Fatal(err)
			}
			if options.RobotTTL != test.want {
				t.Errorf("RobotTTL = %v, want %v", options.RobotTTL, test.want)
			}
		})
	}
}

func TestLoadOptionsEnvOfOtherSubcommand(t *testing.T) {
	t.Setenv("ROBOTEER_HARBOR_URL", "https://harbor.example.com")
	t.Setenv("ROBOTEER_OIDC_ISSUER", "https://issuer.example.com") // only known by "serve"
	flags := pflag.NewFlagSet("cleanup", pflag.ContinueOnError)
	addCommonFlags(flags)

	var o CommonOptions
	if err := LoadOptions(flags, &o); err != nil {
		t.Fatal(err)
	}
	if o.HarborURL.Host != "harbor.example.com" {
		t.Errorf("HarborURL = %v, want harbor.example.com", o.HarborURL)
	}

	t.Setenv("ROBOTEER_HARBOR_URLL", "typo")
	if err := LoadOptions(flags, &o); err == nil {
		t.Error("want error for unknown env var ROBOTEER_HARBOR_URLL")
	}
}
