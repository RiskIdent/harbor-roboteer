package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/RiskIdent/harbor-roboteer/internal/config"
	"github.com/RiskIdent/harbor-roboteer/internal/harbor"
	"github.com/RiskIdent/harbor-roboteer/internal/jwks"
	"github.com/RiskIdent/harbor-roboteer/internal/logger"
	"github.com/RiskIdent/harbor-roboteer/internal/metrics"
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
	"golang.org/x/sync/errgroup"
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

type ServeOptions struct {
	CommonOptions `koanf:",squash"`

	Config       string        `koanf:"config"`
	Addr         string        `koanf:"addr"`
	MetricsAddr  string        `koanf:"metrics-addr"`
	OIDCIssuer   string        `koanf:"oidc-issuer"`
	OIDCAudience string        `koanf:"oidc-audience"`
	RobotTTL     time.Duration `koanf:"robot-ttl"`
}

var ServeDefaults = ServeOptions{
	Config:      "roboteer.jsonnet",
	Addr:        "127.0.0.1:8080",
	MetricsAddr: "127.0.0.1:9090",

	OIDCAudience: "harbor-roboteer",
	RobotTTL:     time.Hour,
}

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Harbor robot account API.",
	Long: `Starts the HTTP API. It validates OIDC tokens, evaluates the configured
policy against their claims, and creates Harbor robot accounts.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		opt, err := LoadServeOptions(cmd.Flags())
		if err != nil {
			return err
		}

		log := logger.New(opt.LogLevel, opt.LogFormat)

		cfg, err := config.Load(opt.Config)
		if err != nil {
			return err
		}
		log.InfoContext(ctx, "Config loaded", slog.String("path", opt.Config))

		traceProvider, err := tracing.NewTracerProvider(ctx, log)
		if err != nil {
			return err
		}
		defer tracing.ShutdownTracer(context.WithoutCancel(ctx), log, traceProvider)

		reg := metrics.NewRegistry()
		ms, err := metrics.NewServer(ctx, log, opt.MetricsAddr, reg)
		if err != nil {
			return err
		}

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

		s, err := server.NewServer(ctx, server.ServerOptions{
			Addr:     opt.Addr,
			Log:      log,
			Config:   cfg,
			Harbor:   hc,
			RobotTTL: opt.RobotTTL,
			JWTOptions: []jwt.ParseOption{
				jwt.WithKeySet(keys.Set),
				jwt.WithIssuer(opt.OIDCIssuer),
				jwt.WithAudience(opt.OIDCAudience),
				jwt.WithAcceptableSkew(time.Minute),
			},
		})
		if err != nil {
			return err
		}
		reg.MustRegister(s.Collectors()...)

		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			return ms.Start(gctx)
		})
		g.Go(func() error {
			return s.Start(gctx)
		})
		if err := g.Wait(); err != nil {
			return fmt.Errorf("run servers: %w", err)
		}

		log.InfoContext(ctx, "server shutdown successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringP("config", "c", ServeDefaults.Config, "config file path, env: $ROBOTEER_CONFIG")
	serveCmd.Flags().StringP("addr", "a", ServeDefaults.Addr, "server addr, env: $ROBOTEER_ADDR")
	serveCmd.Flags().String("metrics-addr", ServeDefaults.MetricsAddr, "metrics server addr, env: $ROBOTEER_METRICS_ADDR")
	addCommonFlags(serveCmd.Flags())
	serveCmd.Flags().String("oidc-issuer", ServeDefaults.OIDCIssuer, "OIDC issuer URL, e.g. https://token.actions.githubusercontent.com, env: $ROBOTEER_OIDC_ISSUER")
	serveCmd.Flags().String("oidc-audience", ServeDefaults.OIDCAudience, "required token audience, e.g. harbor-roboteer, env: $ROBOTEER_OIDC_AUDIENCE")
	serveCmd.Flags().Duration("robot-ttl", ServeDefaults.RobotTTL, "early expiry duration for robot accounts, env: $ROBOTEER_ROBOT_TTL")
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
