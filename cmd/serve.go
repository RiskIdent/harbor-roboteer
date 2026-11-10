package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/jwks"
	"github.com/RiskIdent/harbor-roboteer/internal/logger"
	"github.com/RiskIdent/harbor-roboteer/internal/server"
	"github.com/RiskIdent/harbor-roboteer/internal/tracing"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type ServeOptions struct {
	Config         string   `koanf:"config"`
	Addr           string   `koanf:"addr"`
	LogLevel       string   `koanf:"log-level"`
	LogFormat      string   `koanf:"log-format"`
	HarborURL      *url.URL `koanf:"harbor-url"`
	HarborUsername string   `koanf:"harbor-username"`
	HarborPassword string   `koanf:"harbor-password"`
	OIDCIssuer     string   `koanf:"oidc-issuer"`
	OIDCAudience   string   `koanf:"oidc-audience"`

	Help bool `koanf:"help"` // ignored, but added to keep "ErrorUnused" happy
}

var ServeDefaults = ServeOptions{
	Config:    "roboteer.yaml",
	Addr:      "127.0.0.1:8080",
	LogLevel:  "info",
	LogFormat: "json",
	HarborURL: &url.URL{},

	OIDCAudience: "harbor-roboteer",
}

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
		opt, err := LoadServeOptions(cmd.Flags())
		if err != nil {
			return err
		}

		log := logger.New(opt.LogLevel, opt.LogFormat)

		configWatcher, err := config.Load(ctx, log, opt.Config)
		if err != nil {
			return err
		}
		/* TODO: remove test log */ log.DebugContext(ctx, "config at startup", slog.Any("knownClaims", configWatcher.Config().KnownClaims))

		if err := configWatcher.Watch(ctx); err != nil {
			return fmt.Errorf("watch config file: %w", err)
		}
		defer func() { _ = configWatcher.Close() }()

		traceProvider, err := tracing.NewTracerProvider(ctx, log, cmd.Root().Version)
		if err != nil {
			return err
		}
		defer tracing.ShutdownTracer(context.WithoutCancel(ctx), log, traceProvider)

		hc, err := harbor.New(
			opt.HarborURL,
			opt.HarborUsername,
			opt.HarborPassword,
		)
		if err != nil {
			return err
		}

		if err := (validation.Errors{
			"oidc-issuer":   validation.Validate(opt.OIDCIssuer, validation.Required),
			"oidc-audience": validation.Validate(opt.OIDCAudience, validation.Required),
		}).Filter(); err != nil {
			return fmt.Errorf("OIDC config: %w", err)
		}
		keys, err := jwks.New(ctx, opt.OIDCIssuer)
		if err != nil {
			return err
		}
		defer func() { _ = keys.Close(context.WithoutCancel(ctx)) }()

		s, err := server.NewServer(ctx, opt.Addr, log, hc, configWatcher,
			jwt.WithKeySet(keys.Set),
			jwt.WithIssuer(opt.OIDCIssuer),
			jwt.WithAudience(opt.OIDCAudience),
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

	serveCmd.Flags().StringP("config", "c", ServeDefaults.Config, "config file path, env: $ROBOTEER_CONFIG")
	serveCmd.Flags().StringP("addr", "a", ServeDefaults.Addr, "server addr, env: $ROBOTEER_ADDR")
	serveCmd.Flags().StringP("log-level", "", ServeDefaults.LogLevel, "log level, env: $ROBOTEER_LOG_LEVEL")
	serveCmd.Flags().StringP("log-format", "", ServeDefaults.LogFormat, "log format, env: $ROBOTEER_LOG_FORMAT")
	serveCmd.Flags().String("harbor-url", ServeDefaults.HarborURL.String(), "Harbor base URL, env: $ROBOTEER_HARBOR_URL")
	serveCmd.Flags().String("harbor-username", ServeDefaults.HarborUsername, "Harbor admin or robot username, env: $ROBOTEER_HARBOR_USERNAME")
	serveCmd.Flags().String("harbor-password", ServeDefaults.HarborPassword, "Harbor password, prefer the env var, env: $ROBOTEER_HARBOR_PASSWORD")
	serveCmd.Flags().String("oidc-issuer", ServeDefaults.OIDCIssuer, "OIDC issuer URL, e.g. https://token.actions.githubusercontent.com, env: $ROBOTEER_OIDC_ISSUER")
	serveCmd.Flags().String("oidc-audience", ServeDefaults.OIDCAudience, "required token audience, e.g. harbor-roboteer, env: $ROBOTEER_OIDC_AUDIENCE")
}

func LoadServeOptions(flags *pflag.FlagSet) (*ServeOptions, error) {
	var o ServeOptions
	if err := LoadOptions(flags, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func LoadOptions(flags *pflag.FlagSet, cfgPtr any) error {
	k := koanf.New("-")

	const envPrefix = "ROBOTEER_"
	if err := k.Load(env.Provider("_", env.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, value string) (string, any) {
			return strings.ToLower(strings.TrimPrefix(key, envPrefix)), value
		},
	}), nil); err != nil {
		return fmt.Errorf("read env vars: %w", err)
	}

	if err := k.Load(posflag.Provider(flags, "-", k), nil); err != nil {
		return fmt.Errorf("read command-line flags: %w", err)
	}

	// here's where the actual parsing takes place, like string->*url.URL
	if err := k.UnmarshalWithConf("", cfgPtr, koanf.UnmarshalConf{
		Tag:       "koanf", // it's the default, but just being explicit
		FlatPaths: true,    // allows `koanf:"foo-bar"` instead of requiring sub-structs
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToURLHookFunc(),
			),
			ErrorUnused:      true, // error when user gives env vars we don't support
			WeaklyTypedInput: true,
		},
	}); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	return nil
}
