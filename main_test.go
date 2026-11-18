package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
)

func TestExampleConfigIsValid(t *testing.T) {
	_, err := config.Load(context.Background(), slog.New(slog.DiscardHandler), "roboteer.yaml")
	if err != nil {
		t.Fatalf("roboteer.yaml is invalid:\n%s", err)
	}
}
