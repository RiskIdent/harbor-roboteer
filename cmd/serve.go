package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
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

type ServeOptions struct {
	CommonOptions `koanf:",squash"`

	Config       string        `koanf:"config"`
	Addr         string        `koanf:"addr"`
	MetricsAddr  string        `koanf:"metrics-addr"`
	OIDCIssuers  []string      `koanf:"oidc-issuer"`
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

		if err := validateServeOptions(opt); err != nil {
			return err
		}

		jwtOptionsByIssuer := make(map[string][]jwt.ParseOption, len(opt.OIDCIssuers))
		for _, issuer := range opt.OIDCIssuers {
			if _, exists := jwtOptionsByIssuer[issuer]; exists {
				continue
			}
			keys, err := jwks.New(ctx, issuer)
			if err != nil {
				return err
			}
			defer func() { _ = keys.Close(context.WithoutCancel(ctx)) }()

			jwtOptionsByIssuer[issuer] = []jwt.ParseOption{
				jwt.WithKeySet(keys.Set),
				jwt.WithIssuer(issuer),
				jwt.WithAudience(opt.OIDCAudience),
				jwt.WithAcceptableSkew(time.Minute),
			}
		}

		s, err := server.NewServer(ctx, server.ServerOptions{
			Addr:               opt.Addr,
			Log:                log,
			Config:             cfg,
			Harbor:             hc,
			RobotTTL:           opt.RobotTTL,
			JWTOptionsByIssuer: jwtOptionsByIssuer,
		})
		if err != nil {
			return err
		}
		reg.MustRegister(s.Collectors()...)

		log.InfoContext(ctx, "starting server",
			slog.String("addr", s.Addr().String()),
			slog.String("metricsAddr", ms.Addr().String()))

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
	serveCmd.Flags().StringSlice("oidc-issuer", ServeDefaults.OIDCIssuers, "OIDC issuer URL; repeat for multiple issuers or use comma-separated values, env: $ROBOTEER_OIDC_ISSUER")
	serveCmd.Flags().String("oidc-audience", ServeDefaults.OIDCAudience, "required token audience, e.g. harbor-roboteer, env: $ROBOTEER_OIDC_AUDIENCE")
	serveCmd.Flags().Duration("robot-ttl", ServeDefaults.RobotTTL, "early expiry duration for robot accounts, env: $ROBOTEER_ROBOT_TTL")
}

func LoadServeOptions(flags *pflag.FlagSet) (*ServeOptions, error) {
	var o ServeOptions
	if err := LoadOptions(flags, &o); err != nil {
		return nil, err
	}
	for i := range o.OIDCIssuers {
		o.OIDCIssuers[i] = strings.TrimSpace(o.OIDCIssuers[i])
	}
	o.OIDCIssuers = slices.DeleteFunc(o.OIDCIssuers, func(issuer string) bool {
		return issuer == ""
	})
	return &o, nil
}

func validateServeOptions(opt *ServeOptions) error {
	if err := (validation.Errors{
		"oidc-issuer": validation.Validate(opt.OIDCIssuers,
			validation.Required,
			validation.Each(validation.Required),
		),
		"oidc-audience": validation.Validate(opt.OIDCAudience, validation.Required),
	}).Filter(); err != nil {
		return fmt.Errorf("OIDC config: %w", err)
	}
	return nil
}

func isOtherSubcommandFlag(name string) bool {
	for _, c := range rootCmd.Commands() {
		if c.Flags().Lookup(name) != nil {
			return true
		}
	}
	return false
}

func LoadOptions(flags *pflag.FlagSet, cfgPtr any) error {
	k := koanf.New("-")

	const envPrefix = "ROBOTEER_"
	if err := k.Load(env.Provider("_", env.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, value string) (string, any) {
			key = strings.ToLower(strings.TrimPrefix(key, envPrefix))
			if name := strings.ReplaceAll(key, "_", "-"); flags.Lookup(name) == nil && isOtherSubcommandFlag(name) {
				return "", nil
			}
			return key, value
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
				mapstructure.StringToSliceHookFunc(","),
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
