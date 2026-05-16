package deploy

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CloneRepo clones repoURL into dir.
func CloneRepo(repoURL, dir string) error {
	cmd := exec.Command("git", "clone", repoURL, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// PullLatest runs git pull in dir.
func PullLatest(dir string) error {
	cmd := exec.Command("git", "-C", dir, "pull")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CheckoutBranch checks out branch in dir.
func CheckoutBranch(dir, branch string) error {
	cmd := exec.Command("git", "-C", dir, "checkout", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CheckoutTag checks out tag in dir.
func CheckoutTag(dir, tag string) error {
	cmd := exec.Command("git", "-C", dir, "checkout", "tags/"+tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Procfile maps process type to shell command string.
type Procfile map[string]string

// ParseProcfile reads a Procfile from dir. Returns nil if none exists.
func ParseProcfile(dir string) (Procfile, error) {
	f, err := os.Open(filepath.Join(dir, "Procfile"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	pf := make(Procfile)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		pf[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return pf, scanner.Err()
}

// HasDockerCompose reports whether dir contains a docker-compose file.
func HasDockerCompose(dir string) bool {
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// HasDockerfile reports whether dir contains a Dockerfile.
func HasDockerfile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "Dockerfile"))
	return err == nil
}

// WriteEnvFile ensures dir/.env contains PORT=<port>.
// If .env already exists, PORT is added/updated without touching other entries.
func WriteEnvFile(dir string, port int) error {
	envPath := filepath.Join(dir, ".env")
	portLine := fmt.Sprintf("PORT=%d", port)

	existing, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(envPath, []byte(portLine+"\n"), 0644)
		}
		return fmt.Errorf("failed to read %s: %w", envPath, err)
	}

	lines := strings.Split(string(existing), "\n")
	foundPort := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "PORT=") {
			lines[i] = portLine
			foundPort = true
		}
	}
	if !foundPort {
		lines = append(lines, portLine)
	}
	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
}

// ReadEnvFile parses the .env file in dir.
func ReadEnvFile(dir string) (map[string]string, error) {
	envPath := filepath.Join(dir, ".env")
	existing, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", envPath, err)
	}

	env := make(map[string]string)
	lines := strings.Split(string(existing), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env, nil
}

// SetEnvVars updates specific keys in dir/.env.
func SetEnvVars(dir string, vars map[string]string) error {
	envPath := filepath.Join(dir, ".env")
	existing, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", envPath, err)
	}

	var lines []string
	if len(existing) > 0 {
		lines = strings.Split(string(existing), "\n")
	}

	updated := make(map[string]bool)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			if val, ok := vars[key]; ok {
				lines[i] = fmt.Sprintf("%s=%s", key, val)
				updated[key] = true
			}
		}
	}

	for k, v := range vars {
		if !updated[k] {
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
}

// UnsetEnvVars removes specific keys from dir/.env.
func UnsetEnvVars(dir string, keys []string) error {
	envPath := filepath.Join(dir, ".env")
	existing, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", envPath, err)
	}

	lines := strings.Split(string(existing), "\n")
	removeKeys := make(map[string]bool)
	for _, k := range keys {
		removeKeys[k] = true
	}

	var newLines []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			newLines = append(newLines, line)
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) == 2 && removeKeys[parts[0]] {
			continue
		}
		newLines = append(newLines, line)
	}

	return os.WriteFile(envPath, []byte(strings.Join(newLines, "\n")), 0644)
}

// WriteCompose generates docker-compose.yml for subdomain at port.
// If pf is non-nil, a service is generated per Procfile entry.
// Only the web process gets a port mapping; other processes are internal-only.
func WriteCompose(dir, subdomain string, port int, pf Procfile) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by Vitrina for %s\n", subdomain)
	b.WriteString("services:\n")

	if len(pf) == 0 {
		fmt.Fprintf(&b, "  %s:\n", subdomain)
		b.WriteString("    build: .\n")
		b.WriteString("    restart: unless-stopped\n")
		fmt.Fprintf(&b, "    environment:\n      - PORT=%d\n", port)
		fmt.Fprintf(&b, "    ports:\n      - \"127.0.0.1:%d:%d\"\n", port, port)
		return os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(b.String()), 0644)
	}

	// release: one-shot task (e.g. db:migrate) that web waits on before starting
	if cmd, ok := pf["release"]; ok {
		fmt.Fprintf(&b, "  %s-release:\n", subdomain)
		b.WriteString("    build: .\n")
		b.WriteString("    restart: \"no\"\n")
		fmt.Fprintf(&b, "    command: [\"/bin/sh\", \"-c\", %q]\n", cmd)
	}

	// web: the only process exposed via Caddy
	if webCmd, ok := pf["web"]; ok {
		fmt.Fprintf(&b, "  %s-web:\n", subdomain)
		b.WriteString("    build: .\n")
		b.WriteString("    restart: unless-stopped\n")
		fmt.Fprintf(&b, "    command: [\"/bin/sh\", \"-c\", %q]\n", webCmd)
		fmt.Fprintf(&b, "    environment:\n      - PORT=%d\n", port)
		fmt.Fprintf(&b, "    ports:\n      - \"127.0.0.1:%d:%d\"\n", port, port)
		if _, hasRelease := pf["release"]; hasRelease {
			fmt.Fprintf(&b, "    depends_on:\n      %s-release:\n        condition: service_completed_successfully\n", subdomain)
		}
	}

	// everything else (worker, scheduler, clock, …) — internal, no port mapping
	others := make([]string, 0, len(pf))
	for proc := range pf {
		if proc != "web" && proc != "release" {
			others = append(others, proc)
		}
	}
	sort.Strings(others)

	for _, proc := range others {
		fmt.Fprintf(&b, "  %s-%s:\n", subdomain, proc)
		b.WriteString("    build: .\n")
		b.WriteString("    restart: unless-stopped\n")
		fmt.Fprintf(&b, "    command: [\"/bin/sh\", \"-c\", %q]\n", pf[proc])
	}

	return os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(b.String()), 0644)
}

// ComposeUp runs docker compose up -d --build in dir.
func ComposeUp(dir string) error {
	cmd := exec.Command("docker", "compose", "up", "-d", "--build")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ComposeLogs streams docker compose logs in dir.
func ComposeLogs(dir string, follow bool, tail, since string, services []string) error {
	args := []string{"compose", "logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	if since != "" {
		args = append(args, "--since", since)
	}
	args = append(args, services...)
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ComposePS runs docker compose ps in dir.
func ComposePS(dir string, formatJSON bool) ([]byte, error) {
	args := []string{"compose", "ps"}
	if formatJSON {
		args = append(args, "--format", "json")
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	if formatJSON {
		return cmd.Output()
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return nil, cmd.Run()
}

// ComposeIsRunning checks if there are active containers for the app in dir.
func ComposeIsRunning(dir string) bool {
	cmd := exec.Command("docker", "compose", "ps", "-q")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// ComposeCommand executes a generic docker compose command in dir.
func ComposeCommand(dir string, args ...string) error {
	cmdArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
