package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infkf/vitrina/internal/deploy"
)

func TestParseProcfile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantLen  int
		wantWeb  string
		wantRel  string
	}{
		{
			name:    "simple web only",
			content: "web: npm start\n",
			wantLen: 1,
			wantWeb: "npm start",
		},
		{
			name:    "web and release",
			content: "web: npm start\nrelease: npm run migrate\n",
			wantLen: 2,
			wantWeb: "npm start",
			wantRel: "npm run migrate",
		},
		{
			name: "full Procfile",
			content: `web: bundle exec puma -p $PORT
release: bundle exec rake db:migrate
worker: bundle exec sidekiq
`,
			wantLen: 3,
			wantWeb: "bundle exec puma -p $PORT",
			wantRel: "bundle exec rake db:migrate",
		},
		{
			name:    "empty file",
			content: "",
			wantLen: 0,
		},
		{
			name:    "comments and blank lines",
			content: "# This is a comment\n\nweb: npm start\n# Another comment\n",
			wantLen: 1,
			wantWeb: "npm start",
		},
		{
			name:    "malformed lines skipped",
			content: "web: npm start\nbadline\nok: fine\n",
			wantLen: 2,
			wantWeb: "npm start",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "Procfile"), []byte(tt.content), 0644)

			pf, err := deploy.ParseProcfile(dir)
			if err != nil {
				t.Fatalf("ParseProcfile failed: %v", err)
			}
			if len(pf) != tt.wantLen {
				t.Errorf("expected %d entries, got %d: %v", tt.wantLen, len(pf), pf)
			}
			if tt.wantWeb != "" {
				if cmd := pf["web"]; cmd != tt.wantWeb {
					t.Errorf("web: expected %q, got %q", tt.wantWeb, cmd)
				}
			}
			if tt.wantRel != "" {
				if cmd := pf["release"]; cmd != tt.wantRel {
					t.Errorf("release: expected %q, got %q", tt.wantRel, cmd)
				}
			}
		})
	}
}

func TestParseProcfileNonExistent(t *testing.T) {
	dir := t.TempDir()
	pf, err := deploy.ParseProcfile(dir)
	if err != nil {
		t.Fatalf("ParseProcfile failed: %v", err)
	}
	if pf != nil {
		t.Errorf("expected nil for missing Procfile, got %v", pf)
	}
}

func TestHasDockerCompose(t *testing.T) {
	dir := t.TempDir()

	if deploy.HasDockerCompose(dir) {
		t.Error("expected false for empty dir")
	}

	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:"), 0644)
	if !deploy.HasDockerCompose(dir) {
		t.Error("expected true for docker-compose.yml")
	}

	os.Remove(filepath.Join(dir, "docker-compose.yml"))
	os.WriteFile(filepath.Join(dir, "docker-compose.yaml"), []byte("services:"), 0644)
	if !deploy.HasDockerCompose(dir) {
		t.Error("expected true for docker-compose.yaml")
	}
}

func TestHasDockerfile(t *testing.T) {
	dir := t.TempDir()

	if deploy.HasDockerfile(dir) {
		t.Error("expected false for empty dir")
	}

	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644)
	if !deploy.HasDockerfile(dir) {
		t.Error("expected true")
	}
}

func TestWriteEnvFileNew(t *testing.T) {
	dir := t.TempDir()

	if err := deploy.WriteEnvFile(dir, 3000); err != nil {
		t.Fatalf("WriteEnvFile failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}
	if string(content) != "PORT=3000\n" {
		t.Errorf("expected PORT=3000\\n, got %q", string(content))
	}
}

func TestWriteEnvFileExisting(t *testing.T) {
	dir := t.TempDir()

	existing := "DATABASE_URL=postgres://localhost/db\nSECRET_KEY=abc123\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(existing), 0644)

	if err := deploy.WriteEnvFile(dir, 3000); err != nil {
		t.Fatalf("WriteEnvFile failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "DATABASE_URL") {
		t.Errorf("existing var lost: %s", s)
	}
	if !strings.Contains(s, "SECRET_KEY") {
		t.Errorf("existing var lost: %s", s)
	}
	if !strings.Contains(s, "PORT=3000") {
		t.Errorf("PORT not added: %s", s)
	}
}

func TestWriteEnvFileUpdateExistingPort(t *testing.T) {
	dir := t.TempDir()

	existing := "PORT=8080\nSECRET=xyz\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(existing), 0644)

	if err := deploy.WriteEnvFile(dir, 3000); err != nil {
		t.Fatalf("WriteEnvFile failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}
	s := string(content)
	if strings.Contains(s, "PORT=8080") {
		t.Errorf("old PORT not replaced: %s", s)
	}
	if !strings.Contains(s, "PORT=3000") {
		t.Errorf("new PORT not found: %s", s)
	}
	if !strings.Contains(s, "SECRET=xyz") {
		t.Errorf("existing var lost during PORT update: %s", s)
	}
}

func TestWriteComposeDockerfile(t *testing.T) {
	dir := t.TempDir()

	if err := deploy.WriteCompose(dir, "myapp", 3000, nil); err != nil {
		t.Fatalf("WriteCompose failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("failed to read compose file: %v", err)
	}
	s := string(content)
	for _, want := range []string{
		"myapp:",
		"build: .",
		"restart: unless-stopped",
		"PORT=3000",
		"127.0.0.1:3000:3000",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in output:\n%s", want, s)
		}
	}
}

func TestWriteComposeProcfile(t *testing.T) {
	dir := t.TempDir()

	pf := deploy.Procfile{
		"web":     "npm start",
		"release": "npm run migrate",
		"worker":  "npm run worker",
	}

	if err := deploy.WriteCompose(dir, "myapp", 3000, pf); err != nil {
		t.Fatalf("WriteCompose failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("failed to read compose file: %v", err)
	}
	s := string(content)

	checks := []struct {
		desc string
		want string
	}{
		{"web service", "myapp-web:"},
		{"web port", "127.0.0.1:3000:3000"},
		{"web env", "PORT=3000"},
		{"release service", "myapp-release:"},
		{"release restart no", `restart: "no"`},
		{"release command", "npm run migrate"},
		{"worker service", "myapp-worker:"},
		{"worker no port", "build: ."},
		{"web depends on release", "depends_on:"},
		{"condition", "service_completed_successfully"},
	}
	for _, c := range checks {
		if !strings.Contains(s, c.want) {
			t.Errorf("%s: expected %q in output:\n%s", c.desc, c.want, s)
		}
	}
}

func TestWriteComposeWebOnly(t *testing.T) {
	dir := t.TempDir()

	pf := deploy.Procfile{
		"web": "npm start",
	}

	if err := deploy.WriteCompose(dir, "myapp", 3000, pf); err != nil {
		t.Fatalf("WriteCompose failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("failed to read compose file: %v", err)
	}
	s := string(content)

	if strings.Contains(s, "depends_on") {
		t.Error("no depends_on should be generated when there's no release")
	}
	if strings.Contains(s, "release") {
		t.Error("no release service should be generated")
	}
}
