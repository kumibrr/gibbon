package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// RepoConfig holds per-repo overrides keyed by repo id.
type RepoConfig struct {
	BaseBranch string `toml:"base_branch"`
}

// Config is the workspace configuration stored in .gibbon/config.toml.
type Config struct {
	BranchTemplate string                `toml:"branch_template"`
	Workers        int                   `toml:"workers"`
	Repos          map[string]RepoConfig `toml:"repos"`
}

// DefaultConfig returns the config init writes when nothing is specified.
func DefaultConfig() Config {
	return Config{BranchTemplate: "{feature}", Workers: 8, Repos: map[string]RepoConfig{}}
}

func (w *Workspace) configPath() string { return filepath.Join(w.GibbonDir(), configFile) }

// LoadConfig reads config.toml, filling defaults for missing fields.
func (w *Workspace) LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(w.configPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Repos == nil {
		cfg.Repos = map[string]RepoConfig{}
	}
	if cfg.BranchTemplate == "" {
		cfg.BranchTemplate = "{feature}"
	}
	if cfg.Workers < 1 {
		cfg.Workers = 8
	}
	return cfg, nil
}

// SaveConfig writes config.toml, creating .gibbon/ if needed.
func (w *Workspace) SaveConfig(cfg Config) error {
	if err := os.MkdirAll(w.GibbonDir(), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(w.configPath(), buf.Bytes(), 0o644)
}
