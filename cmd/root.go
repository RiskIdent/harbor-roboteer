package cmd

import (
	"context"
	"os"

	"github.com/RiskIdent/harbor-roboteer/internal/version"
	promversion "github.com/prometheus/common/version"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "harbor-roboteer",
	Short: "Manage Harbor robot accounts for OIDC-authenticated workloads.",
	Long: `harbor-roboteer maps OIDC tokens to Harbor robot accounts, allowing workloads
to authenticate to Harbor without long-lived credentials.

Use the serve subcommand to start the API and metrics servers.`,
	SilenceUsage: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(ctx context.Context) {
	rootCmd.Version = version.Get()
	promversion.Version = rootCmd.Version
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
