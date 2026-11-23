package cmd

import (
	"strings"
	"testing"
)

func TestCommandDescriptions(t *testing.T) {
	tests := []struct {
		name        string
		short       string
		long        string
		shortPhrase string
		longPhrases []string
	}{
		{
			name:        rootCmd.Name(),
			short:       rootCmd.Short,
			long:        rootCmd.Long,
			shortPhrase: "OIDC-authenticated workloads",
			longPhrases: []string{"OIDC tokens", "Harbor robot accounts", "serve subcommand"},
		},
		{
			name:        serveCmd.Name(),
			short:       serveCmd.Short,
			long:        serveCmd.Long,
			shortPhrase: "robot-account service",
			longPhrases: []string{"HTTP API", "OIDC tokens", "metrics endpoint"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(test.short, test.shortPhrase) {
				t.Errorf("Short = %q, want it to describe %q", test.short, test.shortPhrase)
			}
			if strings.Contains(test.short, "brief description") {
				t.Errorf("Short = %q, contains generated placeholder text", test.short)
			}
			if strings.Contains(test.long, "longer description") || strings.Contains(test.long, "Cobra is a CLI library") {
				t.Errorf("Long = %q, contains generated placeholder text", test.long)
			}
			for _, phrase := range test.longPhrases {
				if !strings.Contains(test.long, phrase) {
					t.Errorf("Long = %q, want it to contain %q", test.long, phrase)
				}
			}
		})
	}
}
