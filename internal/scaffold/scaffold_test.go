package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infkf/vitrina/internal/scaffold"
)

func TestGenerateDocker(t *testing.T) {
	dir := t.TempDir()

	app := scaffold.AppInfo{Subdomain: "myapp", Port: 3000}
	if err := scaffold.Generate(dir, "docker", app); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	composePath := filepath.Join(dir, "myapp", "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", composePath, err)
	}
	s := string(content)

	checks := []string{
		"127.0.0.1:3000:3000",
		`PORT: "3000"`,
		"build: .",
		"container_name: myapp",
		"restart: unless-stopped",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Errorf("expected %q in docker-compose output:\n%s", c, s)
		}
	}

	// Should not contain old port contract
	if strings.Contains(s, "8080") {
		t.Error("should not contain hardcoded 8080")
	}

	// Should not contain version field (removed in fix)
	if strings.Contains(s, "version:") {
		t.Error("should not contain deprecated version field")
	}
}

func TestGenerateSystemd(t *testing.T) {
	dir := t.TempDir()

	app := scaffold.AppInfo{Subdomain: "myapp", Port: 3000}
	if err := scaffold.Generate(dir, "systemd", app); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	svcPath := filepath.Join(dir, "myapp", "myapp.service")
	content, err := os.ReadFile(svcPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", svcPath, err)
	}
	s := string(content)

	checks := []string{
		"[Unit]",
		"myapp web application",
		"[Service]",
		"User=www-data",
		"Environment=PORT=3000",
		"[Install]",
		"WantedBy=multi-user.target",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Errorf("expected %q in systemd output:\n%s", c, s)
		}
	}
}

func TestGenerateNone(t *testing.T) {
	dir := t.TempDir()
	app := scaffold.AppInfo{Subdomain: "myapp", Port: 3000}

	if err := scaffold.Generate(dir, "none", app); err != nil {
		t.Fatalf("Generate none failed: %v", err)
	}

	// No files should be created
	entries, _ := os.ReadDir(dir)
	if len(entries) > 0 {
		t.Errorf("expected empty dir for 'none' scaffold, got %d entries", len(entries))
	}
}

func TestGenerateInvalidType(t *testing.T) {
	dir := t.TempDir()
	app := scaffold.AppInfo{Subdomain: "myapp", Port: 3000}

	if err := scaffold.Generate(dir, "invalid", app); err == nil {
		t.Error("expected error for invalid scaffold type")
	}
}

func TestGenerateCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "does", "not", "exist")

	app := scaffold.AppInfo{Subdomain: "myapp", Port: 3000}
	if err := scaffold.Generate(nested, "docker", app); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(nested, "myapp", "docker-compose.yml")); err != nil {
		t.Errorf("compose file should exist: %v", err)
	}
}

func TestGenerateDoesNotVersionField(t *testing.T) {
	dir := t.TempDir()

	app := scaffold.AppInfo{Subdomain: "myapp", Port: 3000}
	if err := scaffold.Generate(dir, "docker", app); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	composePath := filepath.Join(dir, "myapp", "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", composePath, err)
	}

	if strings.Contains(string(content), "version:") {
		t.Error("docker-compose should not contain deprecated 'version' field")
	}
}
