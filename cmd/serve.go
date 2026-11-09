package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/jwks"
	"github.com/RiskIdent/harbor-roboteer/internal/logger"
	"github.com/RiskIdent/harbor-roboteer/internal/server"
	"github.com/RiskIdent/harbor-roboteer/internal/tracing"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/spf13/cobra"
)

var (
	addr      string
	logLevel  string
	logFormat string

	harborURL      string
	harborUsername string
	harborPassword string

	oidcIssuer   string
	oidcAudience string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		log := logger.New(logLevel, logFormat)

		traceProvider, err := tracing.NewTracerProvider(ctx, log, cmd.Root().Version)
		if err != nil {
			return err
		}
		defer tracing.ShutdownTracer(context.WithoutCancel(ctx), log, traceProvider)

		hc, err := harbor.New(
			cmp.Or(harborURL, os.Getenv("HARBOR_URL")),
			cmp.Or(harborUsername, os.Getenv("HARBOR_USERNAME")),
			cmp.Or(harborPassword, os.Getenv("HARBOR_PASSWORD")),
		)
		if err != nil {
			return err
		}

		issuer := cmp.Or(oidcIssuer, os.Getenv("OIDC_ISSUER"))
		audience := cmp.Or(oidcAudience, os.Getenv("OIDC_AUDIENCE"))
		if issuer == "" || audience == "" {
			return errors.New("--oidc-issuer and --oidc-audience are required")
		}
		keys, stopJWKS, err := jwks.New(ctx, issuer)
		if err != nil {
			return err
		}
		defer func() { _ = stopJWKS(context.WithoutCancel(ctx)) }()

		s, err := server.NewServer(ctx, addr, log, cmd.Root().Version, hc,
			jwt.WithKeySet(keys),
			jwt.WithIssuer(issuer),
			jwt.WithAudience(audience),
		)
		if err != nil {
			return err
		}

		if err := s.Start(ctx); err != nil {
			return fmt.Errorf("run server: %w", err)
		}

		log.InfoContext(ctx, "server shutdown successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:8080", "server addr")
	serveCmd.Flags().StringVarP(&logLevel, "log-level", "", "info", "log level")
	serveCmd.Flags().StringVarP(&logFormat, "log-format", "", "json", "log format")
	serveCmd.Flags().StringVar(&harborURL, "harbor-url", "", "Harbor base URL (env HARBOR_URL)")
	serveCmd.Flags().StringVar(&harborUsername, "harbor-username", "", "Harbor admin or robot username (env HARBOR_USERNAME)")
	serveCmd.Flags().StringVar(&harborPassword, "harbor-password", "", "Harbor password, prefer the env var (env HARBOR_PASSWORD)")
	serveCmd.Flags().StringVar(&oidcIssuer, "oidc-issuer", "", "OIDC issuer URL, e.g. https://token.actions.githubusercontent.com (env OIDC_ISSUER)")
	serveCmd.Flags().StringVar(&oidcAudience, "oidc-audience", "", "required token audience, e.g. harbor-roboteer (env OIDC_AUDIENCE)")
}
