package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"vitrina/internal/config"
)

func cleanup(t *testing.T) {
	t.Helper()
	config.ResetBaseDir()
}

func TestSaveAndLoad(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	cfg := &config.Config{
		Domain:       "example.com",
		Email:        "admin@example.com",
		CaddyConfDir: config.DefaultCaddyConfD,
		AppsDir:      config.DefaultAppsDir,
	}

	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", loaded.Domain)
	}
	if loaded.Email != "admin@example.com" {
		t.Errorf("expected email admin@example.com, got %s", loaded.Email)
	}
}

func TestLoadFillsDefaults(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	// Save minimal config with empty optional fields
	cfg := &config.Config{Domain: "example.com", Email: "admin@example.com"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.CaddyConfDir != config.DefaultCaddyConfD {
		t.Errorf("expected default CaddyConfDir, got %s", loaded.CaddyConfDir)
	}
	if loaded.AppsDir != config.DefaultAppsDir {
		t.Errorf("expected default AppsDir, got %s", loaded.AppsDir)
	}
}

func TestLoadMissingDomain(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	cfg := &config.Config{Email: "admin@example.com"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := config.Load(); err == nil {
		t.Error("expected error for missing domain")
	}
}

func TestLoadMissingEmail(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	cfg := &config.Config{Domain: "example.com"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := config.Load(); err == nil {
		t.Error("expected error for missing email")
	}
}

func TestLoadNonExistent(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	if _, err := config.Load(); err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestExists(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	if config.Exists() {
		t.Error("Exists should return false before Save")
	}

	cfg := &config.Config{Domain: "example.com", Email: "admin@example.com"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if !config.Exists() {
		t.Error("Exists should return true after Save")
	}
}

func TestPath(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	expected := filepath.Join(dir, "config.json")
	if config.Path() != expected {
		t.Errorf("expected path %s, got %s", expected, config.Path())
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	cleanup(t)
	dir := filepath.Join(t.TempDir(), "nested", "vitrina")
	config.SetBaseDir(dir)

	cfg := &config.Config{Domain: "example.com", Email: "admin@example.com"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("config directory should exist after Save: %v", err)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	cleanup(t)
	dir := t.TempDir()
	config.SetBaseDir(dir)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(config.Path(), []byte("{invalid"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := config.Load(); err == nil {
		t.Error("expected error for corrupt JSON")
	}
}
