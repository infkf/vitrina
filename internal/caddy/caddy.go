package caddy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/infkf/vitrina/internal/config"
)

const snippetTemplate = `%s {
	reverse_proxy localhost:%d
}
`

func snippetPath(cfg *config.Config, fqdn string) string {
	sanitized := strings.ReplaceAll(fqdn, "*", "wildcard")
	return filepath.Join(cfg.CaddyConfDir, sanitized+".caddy")
}

func WriteAppConfig(cfg *config.Config, fqdn string, port int) error {
	if err := os.MkdirAll(cfg.CaddyConfDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", cfg.CaddyConfDir, err)
	}
	content := fmt.Sprintf(snippetTemplate, fqdn, port)
	path := snippetPath(cfg, fqdn)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func RemoveAppConfig(cfg *config.Config, fqdn string) error {
	path := snippetPath(cfg, fqdn)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

func AppConfigExists(cfg *config.Config, fqdn string) bool {
	_, err := os.Stat(snippetPath(cfg, fqdn))
	return err == nil
}

func ListAppConfigs(cfg *config.Config) ([]string, error) {
	files, err := os.ReadDir(cfg.CaddyConfDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read caddy config dir: %w", err)
	}
	var configs []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".caddy") {
			configs = append(configs, filepath.Join(cfg.CaddyConfDir, f.Name()))
		}
	}
	return configs, nil
}

func Validate(cfg *config.Config) error {
	cmd := exec.Command("caddy", "validate",
		"--config", cfg.Caddyfile,
		"--adapter", "caddyfile",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy validation failed:\n%s", strings.TrimSpace(string(output)))
	}
	return nil
}

func Reload(cfg *config.Config) error {
	methods := [][]string{
		{"caddy", "reload", "--config", cfg.Caddyfile},
		{"systemctl", "reload", "caddy"},
		{"pkill", "-USR1", "caddy"},
	}

	var lastErr error
	for _, m := range methods {
		cmd := exec.Command(m[0], m[1:]...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s: %s", strings.Join(m, " "), strings.TrimSpace(string(output)))
	}

	return fmt.Errorf("all reload methods failed, last error: %w", lastErr)
}

func MainCaddyfileContent(email, confDir string) string {
	return fmt.Sprintf(`# Managed by Vitrina — do not edit manually unless you know what you are doing.

{
	email %s
}

import %s/*
`, email, confDir)
}

func WriteMainCaddyfile(cfg *config.Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.Caddyfile), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(cfg.Caddyfile), err)
	}
	if err := os.MkdirAll(cfg.CaddyConfDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", cfg.CaddyConfDir, err)
	}

	content := MainCaddyfileContent(cfg.Email, cfg.CaddyConfDir)
	if err := os.WriteFile(cfg.Caddyfile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", cfg.Caddyfile, err)
	}
	return nil
}
