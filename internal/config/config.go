package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultConfigDir  = "/etc/vitrina"
	DefaultConfigFile = "config.json"
	DefaultAppsFile   = "apps.json"
	DefaultCaddyDir   = "/etc/caddy"
	DefaultCaddyConfD = "/etc/caddy/conf.d"
	DefaultAppsDir    = "/etc/vitrina/apps"
)

type Config struct {
	Domain       string `json:"domain"`
	Email        string `json:"email"`
	CaddyConfDir string `json:"caddy_conf_dir"`
	AppsDir      string `json:"apps_dir"`
}

var baseDir = DefaultConfigDir

func SetBaseDir(dir string) {
	baseDir = dir
}

func ResetBaseDir() {
	baseDir = DefaultConfigDir
}

func Path() string {
	return filepath.Join(baseDir, DefaultConfigFile)
}

func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w (run 'vitrina init' first)", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config at %s: %w", Path(), err)
	}
	if cfg.CaddyConfDir == "" {
		cfg.CaddyConfDir = DefaultCaddyConfD
	}
	if cfg.AppsDir == "" {
		cfg.AppsDir = DefaultAppsDir
	}
	if cfg.Domain == "" {
		return nil, fmt.Errorf("config at %s is missing required field 'domain'", Path())
	}
	if cfg.Email == "" {
		return nil, fmt.Errorf("config at %s is missing required field 'email'", Path())
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", baseDir, err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(Path(), data, 0644); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", Path(), err)
	}
	return nil
}
