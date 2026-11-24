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
	Short: "Create Harbor robot accounts from OIDC tokens.",
	Long: `harbor-roboteer creates Harbor robot accounts for workloads presenting valid
OIDC tokens. The configured policy maps token claims to account permissions.`,
	Example: `# start the API
harbor-roboteer serve \
  --harbor-url 'https://harbor.example.com' \
  --harbor-username 'foo' \
  --harbor-password 'bar' \
  --oidc-issuer https://token.actions.githubusercontent.com`,
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
