package config

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	KnownClaims []string `yaml:"known_claims"`
}

type Watcher struct {
	path string
	file *file.File
	log  *slog.Logger
}

// Watch starts watching the config file for changes, and calls the callback
// every time it's changed and successfully parsed.
//
// Will stop watching when the context is cancelled,
// and/or when the [Watcher.Close] is called.
func (w *Watcher) Watch(ctx context.Context, callback func(cfg *Config)) error {
	stop := context.AfterFunc(ctx, func() {
		w.Close()
	})
	defer stop()
	return w.file.Watch(func(event any, err error) {
		// "event" is always nil for the file provider
		if err != nil {
			w.log.ErrorContext(ctx, "Error watching config file", slog.String("path", w.path), slog.Any("error", err))
			return
		}
		cfg, err := parseConfig(w.path, w.file)
		if err != nil {
			w.log.ErrorContext(ctx, "Error loading latest config file", slog.String("path", w.path), slog.Any("error", err))
			return
		}

		w.log.InfoContext(ctx, "Config reloaded", slog.String("path", w.path))
		callback(cfg)
	})
}

// Close stops the watcher from watching the file.
func (w *Watcher) Close() error {
	return w.file.Unwatch() // [file.File.Unwatch] uses mutexes, so we don't need to
}

// Load reads the config file, parses it, and returns a watcher to watch for file changes.
//
// Returns an error if the initial load and parse failed.
func Load(ctx context.Context, log *slog.Logger, path string) (*Config, *Watcher, error) {
	f := file.Provider(path)
	cfg, err := parseConfig(path, f)
	if err != nil {
		return nil, nil, err
	}
	log.InfoContext(ctx, "Config loaded", slog.String("path", path))

	return cfg, &Watcher{path: path, file: f, log: log}, nil
}

func parseConfig(path string, f *file.File) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(f, yaml.Parser()); err != nil {
		// don't need to include "path" in error, as [os.ReadFile] already does that
		return nil, fmt.Errorf("load config file: %w", err)
	}

	// drop all "x-*" configs, to allow docker-compose style extensions
	// https://docs.docker.com/reference/compose-file/extension/
	for key := range k.Raw() {
		if strings.HasPrefix(key, "x-") {
			k.Delete(key)
		}
	}

	var cfg Config
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
		Tag: `yaml`,
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.TextUnmarshallerHookFunc(),
			),
			ErrorUnused:     true,
			SquashTagOption: `inline`,
		},
	}); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	return &cfg, nil
}
