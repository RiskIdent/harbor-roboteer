package cmd

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestLoadServeOptionsEarlyExpiry(t *testing.T) {
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
			args: []string{"--early-expiry=2h"},
			want: 2 * time.Hour,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ROBOTEER_EARLY_EXPIRY", test.env)
			flags := pflag.NewFlagSet("serve", pflag.ContinueOnError)
			flags.Duration("early-expiry", ServeDefaults.EarlyExpiry, "")
			if err := flags.Parse(test.args); err != nil {
				t.Fatal(err)
			}

			options, err := LoadServeOptions(flags)
			if err != nil {
				t.Fatal(err)
			}
			if options.EarlyExpiry != test.want {
				t.Errorf("EarlyExpiry = %v, want %v", options.EarlyExpiry, test.want)
			}
		})
	}
}
