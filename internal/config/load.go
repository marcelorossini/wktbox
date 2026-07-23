package config

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

func Load(worktree string, environ map[string]string, overrides Overrides) (Config, error) {
	cfg := Default()
	paths := configPaths(worktree, overrides.ConfigPath)
	for _, candidate := range paths {
		required := overrides.ConfigPath != ""
		data, err := os.ReadFile(candidate)
		if err != nil {
			if os.IsNotExist(err) && !required {
				continue
			}
			return Config{}, fmt.Errorf("read config %s: %w", candidate, err)
		}
		if err := yaml.Load(
			data,
			&cfg,
			yaml.WithKnownFields(),
			yaml.WithUniqueKeys(),
			yaml.WithSingleDocument(),
		); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", candidate, err)
		}
	}

	applyEnvironment(&cfg, environ)
	applyOverrides(&cfg, overrides)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func configPaths(worktree string, alternative string) []string {
	if alternative != "" {
		if !filepath.IsAbs(alternative) {
			alternative = filepath.Join(worktree, alternative)
		}
		return []string{filepath.Clean(alternative)}
	}
	return []string{
		filepath.Join(worktree, ".wktbox.yml"),
		filepath.Join(worktree, ".wktbox.local.yml"),
	}
}

func applyEnvironment(cfg *Config, environ map[string]string) {
	if value := environ["WKTBOX_ENV_FILE"]; value != "" {
		cfg.Environment.File = value
	}
	if value := environ["WKTBOX_ENV_TARGET"]; value != "" {
		cfg.Environment.Target = value
	}
	if value := environ["WKTBOX_DIND_IMAGE"]; value != "" {
		cfg.Runtime.DindImage = value
	}
	if value := environ["WKTBOX_WEBTOP_IMAGE"]; value != "" {
		cfg.Runtime.WebtopImage = value
	}
	if value := environ["WKTBOX_GATEWAY_IMAGE"]; value != "" {
		cfg.Runtime.GatewayImage = value
	}
}

func applyOverrides(cfg *Config, overrides Overrides) {
	if overrides.EnvFile != "" {
		cfg.Environment.File = overrides.EnvFile
	}
	if overrides.EnvTarget != "" {
		cfg.Environment.Target = overrides.EnvTarget
	}
}

func validate(cfg Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version %d; expected version 1", cfg.Version)
	}
	if cfg.Workspace.Target != "/workspace" {
		return fmt.Errorf(
			"unsupported workspace target %q; the MVP requires /workspace",
			cfg.Workspace.Target,
		)
	}
	if cfg.Git.Mode != GitHost && cfg.Git.Mode != GitMounted {
		return fmt.Errorf("unsupported git mode %q", cfg.Git.Mode)
	}
	for name, route := range cfg.Gateway.Routes {
		if route.Port < 1 || route.Port > 65535 {
			return fmt.Errorf("gateway route %q has invalid port %d", name, route.Port)
		}
	}
	return nil
}
