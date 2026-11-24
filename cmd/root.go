package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/RiskIdent/harbor-roboteer/internal/version"
	promversion "github.com/prometheus/common/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommonOptions are the options shared by all subcommands.
type CommonOptions struct {
	LogLevel       string   `koanf:"log-level"`
	LogFormat      string   `koanf:"log-format"`
	HarborURL      *url.URL `koanf:"harbor-url"`
	HarborUsername string   `koanf:"harbor-username"`
	HarborPassword string   `koanf:"harbor-password"`

	Help bool `koanf:"help"` // ignored, but added to keep "ErrorUnused" happy
}

var CommonDefaults = CommonOptions{
	LogLevel:  "info",
	LogFormat: "json",
	HarborURL: &url.URL{},
}

func addCommonFlags(flags *pflag.FlagSet) {
	flags.String("log-level", CommonDefaults.LogLevel, "log level, env: $ROBOTEER_LOG_LEVEL")
	flags.String("log-format", CommonDefaults.LogFormat, "log format, env: $ROBOTEER_LOG_FORMAT")
	flags.String("harbor-url", CommonDefaults.HarborURL.String(), "Harbor base URL, env: $ROBOTEER_HARBOR_URL")
	flags.String("harbor-username", CommonDefaults.HarborUsername, "Harbor admin or robot username, env: $ROBOTEER_HARBOR_USERNAME")
	flags.String("harbor-password", CommonDefaults.HarborPassword, "Harbor password, prefer the env var, env: $ROBOTEER_HARBOR_PASSWORD")
}

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
