package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vspcoderz/provider-hub/internal/schema"
	"gopkg.in/yaml.v3"
)

const (
	DefaultDirName  = "provider-hub"
	DefaultFileName = "providers.yaml"
)

// Path returns the canonical config path.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", DefaultDirName, DefaultFileName), nil
}

// Dir returns the canonical config directory.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", DefaultDirName), nil
}

// Load reads the canonical config, returning defaults if missing.
func Load() (*schema.Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return schema.DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg schema.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// Save writes the canonical config, creating dirs and backing up any existing file.
func Save(cfg *schema.Config) error {
	p, err := Path()
	if err != nil {
		return err
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Backup existing file
	if _, err := os.Stat(p); err == nil {
		backup := fmt.Sprintf("%s.bak.%s", p, time.Now().Format("20060102_150405"))
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read for backup: %w", err)
		}
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// FindProvider returns the provider with the given ID, or nil.
func FindProvider(cfg *schema.Config, id string) *schema.Provider {
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == id {
			return &cfg.Providers[i]
		}
	}
	return nil
}

// UpsertProvider adds or replaces a provider by ID.
func UpsertProvider(cfg *schema.Config, p schema.Provider) {
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == p.ID {
			cfg.Providers[i] = p
			return
		}
	}
	cfg.Providers = append(cfg.Providers, p)
}

// RemoveProvider removes a provider by ID, returning true if found.
func RemoveProvider(cfg *schema.Config, id string) bool {
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == id {
			cfg.Providers = append(cfg.Providers[:i], cfg.Providers[i+1:]...)
			return true
		}
	}
	return false
}
