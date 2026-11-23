package main

import (
	"testing"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
)

func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := config.Load("roboteer.jsonnet")
	if err != nil {
		t.Fatalf("roboteer.jsonnet is invalid:\n%s", err)
	}
	permissions, err := cfg.Evaluate(map[string]any{
		"repository":       "acme/deploy-tools",
		"repository_owner": "acme",
		"environment":      "harbor",
	})
	if err != nil {
		t.Fatalf("roboteer.jsonnet fails to evaluate:\n%s", err)
	}
	if permissions.IsEmpty() {
		t.Fatal("roboteer.jsonnet grants no permissions to an acme repository")
	}
}
