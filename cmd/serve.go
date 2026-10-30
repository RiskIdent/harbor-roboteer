package cmd

import (
	"fmt"

	"github.com/RiskIdent/harbor-roboteer/internal/logger"
	"github.com/RiskIdent/harbor-roboteer/internal/server"
	"github.com/spf13/cobra"
)

var (
	port      string
	logLevel  string
	logFormat string
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
		s := server.NewServer(ctx, port, log)

		if err := s.Start(ctx); err != nil {
			return fmt.Errorf("run server: %w", err)
		}

		log.InfoContext(ctx, "server shutdown successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVarP(&port, "port", "p", "8080", "server port")
	serveCmd.Flags().StringVarP(&logLevel, "log-level", "", "info", "log level")
	serveCmd.Flags().StringVarP(&logFormat, "log-format", "", "json", "log format")
}
