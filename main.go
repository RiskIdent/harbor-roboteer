package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/RiskIdent/harbor-roboteer/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	cmd.Execute(ctx)
}
