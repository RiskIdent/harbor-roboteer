package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/RiskIdent/harbor-roboteer/cmd"
)

var version string // set by ldflags in goreleaser

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	cmd.Execute(ctx, version)
}
