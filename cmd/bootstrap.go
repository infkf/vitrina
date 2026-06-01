package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"vitrina/internal/remote"

	"github.com/spf13/cobra"
)

// installScript runs on the VPS to install Docker and Caddy.
// It is idempotent: already-installed tools are skipped.
const installScript = `#!/bin/sh
set -eu

# Install basic prerequisites first to make bootstrapping robust
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y -q curl gnupg
elif command -v yum >/dev/null 2>&1; then
  yum install -y -q curl gnupg
fi

# ── Docker ────────────────────────────────────────────────────────────────────
if command -v docker >/dev/null 2>&1; then
  echo "docker: already installed"
else
  echo "docker: installing..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq
    apt-get install -y -qq ca-certificates curl gnupg
    install -m 0755 -d /etc/apt/keyrings
    . /etc/os-release
    case "$ID" in
      debian) DOCKER_REPO="https://download.docker.com/linux/debian" ;;
      *)      DOCKER_REPO="https://download.docker.com/linux/ubuntu" ;;
    esac
    curl -fsSL "${DOCKER_REPO}/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    ARCH=$(dpkg --print-architecture)
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] %s %s stable\n' \
      "$ARCH" "$DOCKER_REPO" "$VERSION_CODENAME" | tee /etc/apt/sources.list.d/docker.list >/dev/null
    apt-get update -qq
    apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  elif command -v dnf >/dev/null 2>&1; then
    dnf -y -q install dnf-plugins-core
    dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
    dnf install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q yum-utils
    yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
    yum install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  else
    echo "error: unsupported package manager — install Docker manually: https://docs.docker.com/engine/install/" >&2
    exit 1
  fi
fi
systemctl enable --now docker

# ── Caddy ─────────────────────────────────────────────────────────────────────
if command -v caddy >/dev/null 2>&1; then
  echo "caddy: already installed"
else
  echo "caddy: installing..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
      | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
      | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
    apt-get update -qq
    apt-get install -y -qq caddy
  elif command -v dnf >/dev/null 2>&1; then
    dnf -y -q install dnf-plugins-core
    dnf config-manager --add-repo https://dl.cloudsmith.io/public/caddy/stable/rpm.repo
    dnf install -y -q caddy
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q yum-utils
    yum-config-manager --add-repo https://dl.cloudsmith.io/public/caddy/stable/rpm.repo
    yum install -y -q caddy
  else
    echo "error: unsupported package manager — install Caddy manually: https://caddyserver.com/docs/install" >&2
    exit 1
  fi
fi
systemctl enable caddy

echo "dependencies ready"
`

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap <remote>",
	Short: "Install Docker, Caddy, and Vitrina on a fresh VPS",
	Long: `Connects to the remote VPS over SSH and:
  1. Installs Docker (apt or yum auto-detected) and enables it on boot
  2. Installs Caddy via the official package repo and enables it on boot
  3. Uploads the Vitrina binary
  4. Runs 'vitrina init' with the configured domain and email

The script is idempotent — already-installed tools are skipped.

Domain and email can be stored in the remote profile:
  vitrina remote set prod --host 1.2.3.5 --domain example.com --email me@example.com
  vitrina bootstrap prod

Or passed as flags:
  vitrina bootstrap prod --domain example.com --email me@example.com

The binary to upload must be a Linux binary. If you are on macOS or Windows,
cross-compile first:
  GOOS=linux GOARCH=amd64 go build -o vitrina-linux .
  vitrina bootstrap prod --binary ./vitrina-linux`,
	Args: cobra.ExactArgs(1),
	RunE: runBootstrap,
}

var (
	bootstrapDomain string
	bootstrapEmail  string
	bootstrapBinary string
)

func init() {
	bootstrapCmd.Flags().StringVar(&bootstrapDomain, "domain", "", "Base domain (overrides value stored in remote profile)")
	bootstrapCmd.Flags().StringVar(&bootstrapEmail, "email", "", "Let's Encrypt email (overrides value stored in remote profile)")
	bootstrapCmd.Flags().StringVar(&bootstrapBinary, "binary", "", "Path to Linux vitrina binary to upload (default: current executable)")
	rootCmd.AddCommand(bootstrapCmd)
}

func runBootstrap(_ *cobra.Command, args []string) error {
	remoteName := args[0]

	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	r, ok := cfg.Remotes[remoteName]
	if !ok {
		return fmt.Errorf("remote %q not found — configure it with 'vitrina remote set %s --host <ip>'", remoteName, remoteName)
	}
	r.ApplyDefaults()

	domain := bootstrapDomain
	if domain == "" {
		domain = r.Domain
	}
	if domain == "" {
		return fmt.Errorf("domain required — pass --domain or store it with 'vitrina remote set %s --domain <domain>'", remoteName)
	}

	email := bootstrapEmail
	if email == "" {
		email = r.Email
	}
	if email == "" {
		return fmt.Errorf("email required — pass --email or store it with 'vitrina remote set %s --email <email>'", remoteName)
	}

	binaryPath := bootstrapBinary
	if binaryPath == "" {
		if runtime.GOOS != "linux" {
			built, buildErr := autoBuildLinuxBinary(remoteName)
			if buildErr != nil {
				return buildErr
			}
			defer os.Remove(built)
			binaryPath = built
		} else {
			binaryPath, err = os.Executable()
			if err != nil {
				return fmt.Errorf("could not locate current executable: %w", err)
			}
		}
	}

	fmt.Printf("Bootstrapping %s (%s)...\n\n", remoteName, r.Host)

	fmt.Println("==> Installing dependencies")
	if err := remote.RunScript(r, installScript); err != nil {
		return fmt.Errorf("install script failed: %w", err)
	}

	fmt.Println("\n==> Uploading vitrina binary")
	if err := remote.UploadFile(r, binaryPath, "/tmp/vitrina"); err != nil {
		return fmt.Errorf("binary upload failed: %w", err)
	}
	installCmd := fmt.Sprintf("install -m 0755 /tmp/vitrina %s", r.VitrinaPath)
	if err := remote.RunCommand(r, installCmd); err != nil {
		return fmt.Errorf("failed to install binary to %s: %w", r.VitrinaPath, err)
	}

	fmt.Println("\n==> Initializing Vitrina")
	initCmd := fmt.Sprintf("%s init --domain %s --email %s",
		r.VitrinaPath, domain, email)
	if err := remote.RunCommand(r, initCmd); err != nil {
		return fmt.Errorf("vitrina init failed: %w", err)
	}

	fmt.Printf("\nBootstrap complete. Deploy your first app with:\n")
	fmt.Printf("  vitrina -r %s deploy <subdomain> <git-url>\n", remoteName)
	return nil
}

// autoBuildLinuxBinary cross-compiles a linux/amd64 binary from the source
// tree (located by finding go.mod above the cwd). Returns the temp file path.
func autoBuildLinuxBinary(remoteName string) (string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return "", fmt.Errorf(
			"you are on %s but the VPS needs a Linux binary\n"+
				"'go' was not found in PATH — cross-compile manually:\n"+
				"  GOOS=linux GOARCH=amd64 go build -o vitrina-linux .\n"+
				"  vitrina bootstrap %s --binary ./vitrina-linux",
			runtime.GOOS, remoteName,
		)
	}

	srcDir, err := findGoModDir()
	if err != nil {
		return "", fmt.Errorf(
			"you are on %s but the VPS needs a Linux binary\n"+
				"could not find go.mod — cross-compile manually:\n"+
				"  GOOS=linux GOARCH=amd64 go build -o vitrina-linux .\n"+
				"  vitrina bootstrap %s --binary ./vitrina-linux",
			runtime.GOOS, remoteName,
		)
	}

	tmp, err := os.CreateTemp("", "vitrina-linux-*")
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	tmp.Close()

	fmt.Println("==> Building Linux binary...")
	build := exec.Command("go", "build", "-o", tmp.Name(), ".")
	build.Dir = srcDir
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("auto-build failed: %w", err)
	}
	return tmp.Name(), nil
}

func findGoModDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
