package caddy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Domain:       "example.com",
		Email:        "admin@example.com",
		CaddyConfDir: "/tmp/test-caddy-conf.d",
		AppsDir:      "/tmp/test-apps",
	}
}

func TestMainCaddyfileContent(t *testing.T) {
	content := caddy.MainCaddyfileContent("admin@example.com")

	if !strings.Contains(content, "admin@example.com") {
		t.Errorf("expected email in Caddyfile:\n%s", content)
	}
	if !strings.Contains(content, "import") {
		t.Errorf("expected import directive:\n%s", content)
	}
	if !strings.Contains(content, config.DefaultCaddyConfD) {
		t.Errorf("expected conf.d path:\n%s", content)
	}
}

func TestWriteAndRemoveAppConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.CaddyConfDir = dir

	fqdn := "myapp.example.com"
	if err := caddy.WriteAppConfig(cfg, fqdn, 3000); err != nil {
		t.Fatalf("WriteAppConfig failed: %v", err)
	}

	if !caddy.AppConfigExists(cfg, fqdn) {
		t.Error("AppConfigExists should return true after write")
	}

	// Verify content
	path := filepath.Join(dir, "myapp.example.com.caddy")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read snippet: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, fqdn) {
		t.Errorf("expected FQDN %q in snippet", fqdn)
	}
	if !strings.Contains(s, "reverse_proxy localhost:3000") {
		t.Errorf("expected reverse_proxy directive:\n%s", s)
	}

	// Remove
	if err := caddy.RemoveAppConfig(cfg, fqdn); err != nil {
		t.Fatalf("RemoveAppConfig failed: %v", err)
	}
	if caddy.AppConfigExists(cfg, fqdn) {
		t.Error("AppConfigExists should return false after remove")
	}
}

func TestRemoveAppConfigNonExistent(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.CaddyConfDir = dir

	if err := caddy.RemoveAppConfig(cfg, "nonexistent.example.com"); err != nil {
		t.Errorf("RemoveAppConfig should not error on missing file: %v", err)
	}
}

func TestAppConfigExistsNonExistent(t *testing.T) {
	cfg := testConfig()
	if caddy.AppConfigExists(cfg, "nonexistent.example.com") {
		t.Error("should return false for nonexistent config")
	}
}

func TestSnippetPathWildcard(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.CaddyConfDir = dir

	fqdn := "*.example.com"
	if err := caddy.WriteAppConfig(cfg, fqdn, 3000); err != nil {
		t.Fatalf("WriteAppConfig failed: %v", err)
	}

	// The file should use "wildcard" instead of "*" in the path
	if _, err := os.Stat(filepath.Join(dir, "*.example.com.caddy")); err == nil {
		t.Error("file with literal * in name should not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "wildcard.example.com.caddy")); err != nil {
		t.Error("file with wildcard replacement should exist")
	}
}

func TestWriteAppConfigCreatesDir(t *testing.T) {
	base := t.TempDir()
	cfg := testConfig()
	cfg.CaddyConfDir = filepath.Join(base, "conf.d")

	if err := caddy.WriteAppConfig(cfg, "myapp.example.com", 3000); err != nil {
		t.Fatalf("WriteAppConfig failed: %v", err)
	}

	if _, err := os.Stat(cfg.CaddyConfDir); err != nil {
		t.Errorf("CaddyConfDir should exist: %v", err)
	}
}

func TestAppConfigContent(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.CaddyConfDir = dir

	if err := caddy.WriteAppConfig(cfg, "myapp.example.com", 3000); err != nil {
		t.Fatalf("WriteAppConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "myapp.example.com.caddy"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	expected := "myapp.example.com {\n\treverse_proxy localhost:3000\n}\n"
	if string(content) != expected {
		t.Errorf("got:\n%q\nwant:\n%q", string(content), expected)
	}
}
