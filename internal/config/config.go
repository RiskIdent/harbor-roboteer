package config

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	KnownClaims []string `yaml:"known_claims"`

	Identities []Identity `yaml:"identities"`

	Projects Projects `yaml:"projects"`

	AllProjects AllProjects `yaml:"all_projects"`

	System System `yaml:"system"`
}

type Identity map[string]Pattern

type Projects struct {
	// Map of roboteer project roles. These can be referenced in the grants to deduplicate your config.
	Roles map[string]ProjectPermissions `yaml:"roles"`
	// Map of permission grants. The map key is the grant "ID" (shown in audit logs).
	// All matching grants add up; nothing denies.
	Grants map[string]ProjectGrant `yaml:"grants"`
}

type ProjectPermissions map[string][]string

type ProjectGrant struct {
	When        Condition          `yaml:"when,omitempty"`
	Projects    []Pattern          `yaml:"projects"`
	Roles       []Pattern          `yaml:"roles"`
	Permissions ProjectPermissions `yaml:"permissions"`
}

type AllProjects struct {
	// Grants on all projects, including ones created later ("Cover all projects" in Harbor).
	// Uses the project roles.
	// All matching grants add up; nothing denies.
	Grants map[string]ProjectGrant `yaml:"grants"`
}

type System struct {
	// Map of roboteer system-scope roles. These can be referenced in the grants to deduplicate your config.
	Roles map[string]SystemPermissions `yaml:"roles"`
	// Map of permission grants. The map key is the grant "ID" (shown in audit logs).
	// All matching grants add up; nothing denies.
	Grants map[string]SystemGrant `yaml:"grants"`
}

type SystemPermissions map[string][]string

type SystemGrant struct {
	When        Condition         `yaml:"when,omitempty"`
	Roles       []Pattern         `yaml:"roles"`
	Permissions SystemPermissions `yaml:"permissions"`
}

type Condition map[string]string

type Watcher struct {
	path       string
	file       *file.File
	log        *slog.Logger
	cfg        *Config
	mutex      sync.RWMutex
	isWatching bool
}

func (w *Watcher) Config() *Config {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.cfg
}

func (w *Watcher) IsWatching() bool {
	return w.isWatching
}

// Watch starts watching the config file for changes, and calls the callback
// every time it's changed and successfully parsed.
//
// Will stop watching when the context is cancelled,
// and/or when the [Watcher.Close] is called.
func (w *Watcher) Watch(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = w.Close()
	}()
	w.isWatching = true
	return w.file.Watch(func(event any, err error) {
		// "event" is always nil for the file provider
		if err != nil {
			w.log.ErrorContext(ctx, "Error watching config file", slog.String("path", w.path), slog.Any("error", err))
			w.isWatching = false // koanf stops the watch loop here
			return
		}
		cfg, err := parseConfig(w.path, w.file)
		if err != nil {
			w.log.ErrorContext(ctx, "Error loading latest config file", slog.String("path", w.path), slog.Any("error", err))
			return
		}

		w.mutex.Lock()
		defer w.mutex.Unlock()
		w.cfg = cfg
		w.log.InfoContext(ctx, "Config reloaded", slog.String("path", w.path))
	})
}

// Close stops the watcher from watching the file.
func (w *Watcher) Close() error {
	w.isWatching = false
	return w.file.Unwatch() // [file.File.Unwatch] uses mutexes, so we don't need to
}

// Load reads the config file, parses it, and returns a watcher to watch for file changes.
//
// Returns an error if the initial load and parse failed.
func Load(ctx context.Context, log *slog.Logger, path string) (*Watcher, error) {
	f := file.Provider(path)
	cfg, err := parseConfig(path, f)
	if err != nil {
		return nil, err
	}
	log.InfoContext(ctx, "Config loaded", slog.String("path", path))

	return &Watcher{
		path: path,
		file: f,
		log:  log,
		cfg:  cfg,
	}, nil
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
