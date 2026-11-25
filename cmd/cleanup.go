package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/logger"
	"github.com/RiskIdent/harbor-roboteer/internal/tracing"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Delete expired robot accounts created by harbor-roboteer.",
	Long: `Deletes every robot account created by harbor-roboteer whose expiry has passed.
Failed deletions are reported but don't stop the run, so it is safe to run as a cron job.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		var opt CommonOptions
		if err := LoadOptions(cmd.Flags(), &opt); err != nil {
			return err
		}

		log := logger.New(opt.LogLevel, opt.LogFormat)

		traceProvider, err := tracing.NewTracerProvider(ctx, log)
		if err != nil {
			return err
		}
		defer tracing.ShutdownTracer(context.WithoutCancel(ctx), log, traceProvider)

		ctx, span := otel.Tracer("harbor-roboteer").Start(ctx, "cleanup")
		defer span.End()

		hc, err := harbor.New(opt.HarborURL, opt.HarborUsername, opt.HarborPassword)
		if err != nil {
			return err
		}

		deleted, err := hc.CleanupRobots(ctx, time.Now())
		log.InfoContext(ctx, "Cleanup finished", slog.Int("deleted", deleted))
		return err
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)

	addCommonFlags(cleanupCmd.Flags())
}
