package deploy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CloneRepo clones repoURL into dir. When quiet, git progress is suppressed.
func CloneRepo(repoURL, dir string, quiet bool) error {
	args := []string{"clone"}
	if quiet {
		args = append(args, "--quiet")
	}
	args = append(args, repoURL, dir)
	cmd := exec.Command("git", args...)
	if !quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// IsTag reports whether ref is a valid git tag in the repository.
func IsTag(dir, ref string) bool {
	cmd := exec.Command("git", "-C", dir, "show-ref", "--tags", "refs/tags/"+ref)
	return cmd.Run() == nil
}

// Fetch runs git fetch origin --tags in dir.
func Fetch(dir string, quiet bool) error {
	cmd := exec.Command("git", "-C", dir, "fetch", "origin", "--tags")
	if !quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// PullLatest runs git pull in dir, or fetches and checkouts if ref is a tag.
func PullLatest(dir string, ref string, quiet bool) error {
	if ref != "" && IsTag(dir, ref) {
		if err := Fetch(dir, quiet); err != nil {
			return err
		}
		return CheckoutTag(dir, ref)
	}

	if ref != "" {
		if err := CheckoutBranch(dir, ref); err != nil {
			return fmt.Errorf("failed to checkout branch %q: %w", ref, err)
		}
	}

	var stderrBuf strings.Builder
	cmd := exec.Command("git", "-C", dir, "pull")
	if quiet {
		cmd.Stderr = &stderrBuf
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}
	if err := cmd.Run(); err != nil {
		errStr := stderrBuf.String()
		if strings.Contains(errStr, "diverged") || strings.Contains(errStr, "divergent") {
			return forcePull(dir, ref, quiet)
		}
		if strings.Contains(errStr, "untracked") || strings.Contains(errStr, "overwritten") ||
			strings.Contains(errStr, "local changes") || strings.Contains(errStr, "merge") {
			return fmt.Errorf("git pull failed — repository has local changes or conflicts; use '--force' to discard and sync with remote: %w", err)
		}
		return err
	}
	return nil
}

// ForcePull syncs dir to the remote HEAD without requiring a clean history
// (git fetch + reset --hard). Use for force-pushed branches or tags.
func ForcePull(dir string, ref string, quiet bool) error {
	if !quiet {
		fmt.Println("Force-syncing to remote (fetch + reset --hard)...")
	}
	return forcePull(dir, ref, quiet)
}

func forcePull(dir string, ref string, quiet bool) error {
	if err := Fetch(dir, quiet); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	targetRef := ref
	if targetRef == "" {
		targetRef = currentBranch(dir)
	}

	if IsTag(dir, targetRef) {
		reset := exec.Command("git", "-C", dir, "reset", "--hard", "tags/"+targetRef)
		if !quiet {
			reset.Stdout = os.Stdout
			reset.Stderr = os.Stderr
		}
		return reset.Run()
	}

	reset := exec.Command("git", "-C", dir, "reset", "--hard", "origin/"+targetRef)
	if !quiet {
		reset.Stdout = os.Stdout
		reset.Stderr = os.Stderr
	}
	return reset.Run()
}

func currentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
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
		b.WriteString("    env_file: .env\n")
		fmt.Fprintf(&b, "    ports:\n      - \"127.0.0.1:%d:%d\"\n", port, port)
		return os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(b.String()), 0644)
	}

	// release: one-shot task (e.g. db:migrate) that web waits on before starting
	if cmd, ok := pf["release"]; ok {
		fmt.Fprintf(&b, "  %s-release:\n", subdomain)
		b.WriteString("    build: .\n")
		b.WriteString("    restart: \"no\"\n")
		fmt.Fprintf(&b, "    command: [\"/bin/sh\", \"-c\", %q]\n", cmd)
		b.WriteString("    env_file: .env\n")
	}

	// web: the only process exposed via Caddy
	if webCmd, ok := pf["web"]; ok {
		fmt.Fprintf(&b, "  %s-web:\n", subdomain)
		b.WriteString("    build: .\n")
		b.WriteString("    restart: unless-stopped\n")
		fmt.Fprintf(&b, "    command: [\"/bin/sh\", \"-c\", %q]\n", webCmd)
		fmt.Fprintf(&b, "    environment:\n      - PORT=%d\n", port)
		b.WriteString("    env_file: .env\n")
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
		b.WriteString("    env_file: .env\n")
	}

	return os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(b.String()), 0644)
}

// ComposeUp runs docker compose up -d --build in dir.
// When quiet, output is suppressed and a timing summary is printed instead.
// On failure in quiet mode, captured output is shown to aid debugging.
func ComposeUp(dir string, quiet bool) error {
	cmd := exec.Command("docker", "compose", "up", "-d", "--build")
	cmd.Dir = dir
	if !quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	start := time.Now()
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		fmt.Print(buf.String())
		return err
	}
	fmt.Printf("Built and started (%.0fs)\n", time.Since(start).Seconds())
	return nil
}

// ComposeRestart runs docker compose up -d (without --build) to apply
// configuration changes such as updated env vars without rebuilding images.
func ComposeRestart(dir string, quiet bool) error {
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = dir
	if !quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		fmt.Print(buf.String())
		return err
	}
	return nil
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

// ComposeLogsStream returns a command that streams logs, piping output through
// the given writer. Caller is responsible for starting and waiting on the command.
func ComposeLogsStream(dir string, follow bool, tail, since string, services []string, w io.Writer) *exec.Cmd {
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
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd
}

// ComposeLogsPipe starts docker compose logs and returns the stdout pipe.
// The caller must Start() the command and close the pipe when done.
func ComposeLogsPipe(dir string, follow bool, tail, since string, services []string) (io.ReadCloser, error) {
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

	r, w := io.Pipe()
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		w.Close()
		return nil, err
	}

	go func() {
		cmd.Wait()
		w.Close()
	}()

	return r, nil
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

// ComposeCommandQuiet is like ComposeCommand but captures output.
func ComposeCommandQuiet(dir string, args ...string) (string, error) {
	cmdArgs := append([]string{"compose"}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ComposeUpWithPort starts containers with a custom project name and port override.
// It reads the existing compose file, replaces port mappings and PORT assignments
// from oldPort to port, and writes a temporary override compose file.
func ComposeUpWithPort(dir, projectName string, port, oldPort int, quiet bool) error {
	composePath := findComposeFile(dir)
	if composePath == "" {
		return fmt.Errorf("no docker-compose.yml or docker-compose.yaml found in %s", dir)
	}

	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	oldPortStr := fmt.Sprintf("%d", oldPort)
	portStr := fmt.Sprintf("%d", port)
	content := strings.ReplaceAll(string(data), ":"+oldPortStr+":"+oldPortStr, ":"+portStr+":"+portStr)
	content = strings.ReplaceAll(content, "PORT="+oldPortStr, "PORT="+portStr)

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s-compose-*.yml", projectName))
	if err != nil {
		return fmt.Errorf("failed to create temp compose file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp compose file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command("docker", "compose",
		"--project-name", projectName,
		"-f", tmpPath,
		"up", "-d", "--build",
	)
	cmd.Dir = dir

	if quiet {
		var buf strings.Builder
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		start := time.Now()
		if err := cmd.Run(); err != nil {
			fmt.Print(buf.String())
			return err
		}
		fmt.Printf("  green: built and started (%.0fs)\n", time.Since(start).Seconds())
		return nil
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findComposeFile(dir string) string {
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// ComposeDownProject tears down containers for a specific compose project.
func ComposeDownProject(dir, projectName string) error {
	cmd := exec.Command("docker", "compose",
		"--project-name", projectName,
		"down", "-v",
	)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compose down %s: %v\n%s", projectName, err, string(out))
	}
	return nil
}

// ComposeStopProject stops containers for a specific compose project.
func ComposeStopProject(dir, projectName string) error {
	cmd := exec.Command("docker", "compose",
		"--project-name", projectName,
		"stop",
	)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compose stop %s: %v\n%s", projectName, err, string(out))
	}
	return nil
}

// ComposeIsRunningProject checks if a specific compose project has running containers.
func ComposeIsRunningProject(dir, projectName string) bool {
	cmd := exec.Command("docker", "compose",
		"--project-name", projectName,
		"ps", "-q",
	)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// WaitForPort polls a TCP port until it accepts connections or timeout is reached.
func WaitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("port %d did not become ready within %v", port, timeout)
}

// BlueGreenDeploy performs a zero-downtime deploy:
//  1. Build and start new containers on greenPort with project "<sub>-green"
//  2. Wait for greenPort to accept connections
//  3. Call swapFn (update Caddy, reload, update registry)
//  4. Tear down old compose project
//
// Returns the green port on success.
func BlueGreenDeploy(dir, subdomain string, greenPort int, oldPort int, quiet bool, swapFn func() error) error {
	greenProject := subdomain + "-green"

	fmt.Printf("  Blue (active): port %d\n", oldPort)
	fmt.Printf("  Green (new):   port %d\n", greenPort)

	if err := ComposeUpWithPort(dir, greenProject, greenPort, oldPort, quiet); err != nil {
		return fmt.Errorf("green deploy failed: %w", err)
	}

	if err := WaitForPort(greenPort, 30*time.Second); err != nil {
		_ = ComposeDownProject(dir, greenProject)
		return fmt.Errorf("green health check failed: %w", err)
	}
	fmt.Printf("  Green healthy on port %d\n", greenPort)

	if err := swapFn(); err != nil {
		_ = ComposeDownProject(dir, greenProject)
		return fmt.Errorf("swap failed, green torn down: %w", err)
	}
	fmt.Printf("  Swapped Caddy to green port %d\n", greenPort)

	fmt.Printf("  Tearing down blue (port %d)...\n", oldPort)
	_ = ComposeStopProject(dir, subdomain)
	_ = ComposeDownProject(dir, subdomain)

	return nil
}

type VitrinaMarker struct {
	Subdomain  string `json:"subdomain"`
	FQDN       string `json:"fqdn"`
	Port       int    `json:"port"`
	ManagedBy  string `json:"managed_by"`
	DeployedBy string `json:"deployed_by,omitempty"`
	GitURL     string `json:"git_url,omitempty"`
	GitRef     string `json:"git_ref,omitempty"`
}

func WriteVitrinaMarker(dir string, marker *VitrinaMarker) error {
	marker.ManagedBy = "vitrina"
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .vitrina.json: %w", err)
	}
	path := filepath.Join(dir, ".vitrina.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
