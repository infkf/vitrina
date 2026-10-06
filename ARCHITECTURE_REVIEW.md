# Architectural Review

`go test ./...` and `go vet ./...` pass. The main architectural concern is that deployment state spans several independent systems: registry JSON, Caddy files, Git checkout, Docker Compose, and environment files. There is no durable transaction or reconciliation model tying them together.

## 1. AI-Agent Smoothness

### High: Structured output is incomplete and nondeterministic

`--json` exists globally, but only `list`, `status`, `ps`, and `env list` consistently use it. Mutating commands still emit human text, timers, ANSI formatting, and warnings. MCP handlers shell out and parse combined terminal output (`internal/mcp/server.go:35-54`).

This makes automated agents dependent on unstable strings.

**Current pattern:**

```go
fmt.Printf("Deployed: https://%s\n", fqdn)
return nil
```

**Recommended pattern:**

```go
type Result struct {
    OK        bool   `json:"ok"`
    Operation string `json:"operation"`
    App       any    `json:"app,omitempty"`
    Error     *Error `json:"error,omitempty"`
}

type Error struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

func writeResult(cmd *cobra.Command, result Result) error {
    if jsonOutput {
        return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
    }
    fmt.Fprintf(cmd.OutOrStdout(), "Deployed: %v\n", result.App)
    return nil
}
```

Recommended behavior:

- Add `--format human|json` or consistently support `--json` on every command.
- Send all human progress output to stderr.
- Reserve stdout for the machine-readable result.
- Make JSON schemas stable and versioned.

### High: Error classification is too weak

`cmd.Execute` maps every error to exit code `1` (`cmd/root.go:42-46`). Remote failures return raw `*exec.ExitError` values (`internal/remote/remote.go:126-131`), and most application errors are only distinguishable by message text.

Agents need to distinguish validation failures, authentication failures, unavailable services, deployment failures, and partial success.

**Current pattern:**

```go
if err := rootCmd.Execute(); err != nil {
    output.Error(err.Error())
    os.Exit(1)
}
```

**Recommended pattern:**

```go
type ExitError struct {
    Code       string
    ExitStatus int
    Err        error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

var (
    ErrValidation = errors.New("validation failed")
    ErrRemote     = errors.New("remote operation failed")
    ErrPartial    = errors.New("partial deployment")
)

func exitCode(err error) int {
    var ee *ExitError
    if errors.As(err, &ee) {
        return ee.ExitStatus
    }
    return 1
}
```

Use stable codes such as `INVALID_ARGUMENT`, `REMOTE_UNAVAILABLE`, `CADDY_INVALID`, `DEPLOY_PARTIAL`, and `ROLLBACK_FAILED`.

### High: Idempotency and concurrency are incomplete

`deploy` rejects an existing app instead of reconciling desired state (`cmd/deploy.go:69-75`). `NextFreePort` checks availability and later registration occurs in a separate operation (`internal/registry/registry.go:177-197`), so concurrent invocations can select the same port.

The registry is process-safe only through an in-memory mutex. Multiple Vitrina processes can overwrite `apps.json`, and writes are not atomic (`registry.go:67-78`).

**Recommended atomic persistence:**

```go
func atomicWrite(path string, data []byte, mode fs.FileMode) error {
    tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
    if err != nil {
        return err
    }
    tmpName := tmp.Name()
    defer os.Remove(tmpName)

    if err := tmp.Chmod(mode); err != nil {
        tmp.Close()
        return err
    }
    if _, err := tmp.Write(data); err != nil {
        tmp.Close()
        return err
    }
    if err := tmp.Sync(); err != nil {
        tmp.Close()
        return err
    }
    if err := tmp.Close(); err != nil {
        return err
    }
    return os.Rename(tmpName, path)
}
```

Also add:

- A filesystem lock around registry mutations.
- An atomic `ReservePortAndApp` operation.
- Idempotent deploy semantics based on subdomain plus desired Git ref or deployment ID.
- A `plan` or `diff` command before mutation.
- A persistent deployment record with phase and status.

### Medium: Remote command construction is brittle

Remote forwarding reconstructs arguments from `os.Args` (`internal/remote/remote.go:211-240`). This is fragile for nested commands, flag values, repeated flags, and future Cobra behavior.

Use Cobra’s parsed command state or pass an explicit argument vector into the remote executor rather than reparsing process arguments.

Additionally, `export` and `import` build shell scripts using `r.VitrinaPath` without shell escaping (`cmd/export.go:64`, `cmd/import.go:66`). A corrupted or malicious remote profile can inject shell syntax.

**Recommended pattern:**

```go
func shellCommand(path string, args ...string) string {
    parts := []string{shellEscape(path)}
    for _, arg := range args {
        parts = append(parts, shellEscape(arg))
    }
    return strings.Join(parts, " ")
}
```

### Medium: Headless execution is mostly supported, but secrets are unsafe

There are no obvious interactive application prompts, but:

- `ssh` and `sudo` inherit stdin in several paths.
- Sudo may block waiting for a password.
- `env set KEY=VALUE` exposes secrets in shell history and process arguments.
- `env list` prints secret values.
- `.env` files are created with mode `0644` (`internal/deploy/deploy.go:207`, `287`, `321`).
- `export` deliberately archives `.env` files (`cmd/export.go:145-147`).

**Recommended pattern:**

```go
func writeSecretFile(path string, data []byte) error {
    f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
    if err != nil {
        return err
    }
    defer f.Close()

    if _, err := f.Write(data); err != nil {
        return err
    }
    return f.Sync()
}
```

Add `env set --from-file` and stdin support, make `env list --keys` the default, require explicit `--show-values`, redact secrets in JSON and logs, and use `sudo -n` for guaranteed non-interactive behavior.

## 2. Human Smoothness

### High: Secret handling violates least privilege

The combination of plaintext `.env`, `0644` permissions, plaintext environment listing, and backups containing `.env` is a serious operational risk.

At minimum:

```go
if err := os.Chmod(envPath, 0600); err != nil {
    return fmt.Errorf("secure env file: %w", err)
}
```

Backups should either encrypt secrets or exclude them by default:

```go
if includeSecrets {
    addFileToTar(envPath, tarPath)
}
```

Make secret inclusion explicit, for example `export --include-secrets`.

### Medium: Logging has no levels or injectable output

`internal/output/output.go` writes directly to stdout and stderr. There is no debug/info/warn/error level, no logger injection, and no consistent handling of machine output. Long operations such as SCP only expose the underlying command’s output (`internal/remote/remote.go:172-179`).

**Recommended pattern:**

```go
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}

type Options struct {
    Quiet   bool
    Verbose bool
    JSON    bool
}
```

Use structured fields for deployment identity, remote, app, phase, and elapsed time. Progress should be reported on stderr and disabled in JSON mode.

### Medium: Configuration is not project-aware

Configuration is JSON-only and primarily stored in `/etc/vitrina` or `~/.vitrina`. There is no project manifest describing build mode, health endpoint, environment source, deployment strategy, resource limits, restart policy, or rollback policy.

A project-level `.vitrina.yaml` could provide reproducible deployment settings while keeping secrets external:

```yaml
app: api
health_path: /healthz
deploy:
  strategy: blue-green
  branch: main
  timeout: 5m
env:
  file: .env.production
```

Validate configuration explicitly:

```go
type ProjectConfig struct {
    App        string       `yaml:"app"`
    HealthPath string       `yaml:"health_path"`
    Deploy     DeployConfig `yaml:"deploy"`
}

func (c ProjectConfig) Validate() error {
    if c.App == "" {
        return errors.New("app is required")
    }
    if c.Deploy.Timeout <= 0 {
        return errors.New("deploy.timeout must be positive")
    }
    return nil
}
```

### Medium: Existing command ergonomics are inconsistent

The Cobra command layout is reasonable, but behavior varies:

- Some commands support `--json`; others ignore the global flag.
- `push` has separate local and remote code paths.
- `remove --clean` is destructive but has no dry-run or confirmation mode.
- `doctor --heal` performs broad mutations with no plan output.
- `remote list` iterates a map without sorting (`cmd/remote.go:136-145`), producing nondeterministic output.

Add `--dry-run` for mutating commands, `--yes` for destructive operations, sorted output everywhere, consistent timeout and verbosity flags, and a common command execution layer.

## 3. Deployment Reliability and Core Engineering

### Critical: No end-to-end transaction or reliable rollback

Deployment mutates state in several independent phases:

1. Clone or modify the Git checkout.
2. Write `.env` and Compose files.
3. Write Caddy configuration.
4. Register the app.
5. Start Docker.
6. Reload Caddy.

Failures are compensated inconsistently:

- `redeploy` changes Git before building; a failed build leaves the checkout changed.
- `deploy` treats Caddy reload failure as a warning and returns success (`cmd/deploy.go:184-187`).
- Blue/green can leave Caddy and Docker on green while registry metadata remains on blue (`cmd/redeploy.go:129-149`).
- Rollback calls `docker compose down -v`, which can delete persistent volumes (`cmd/deploy.go:175-179`).
- `push` deletes existing application contents before extraction (`cmd/push.go:100-103`, `147-152`).

Use deployment generations and a journal:

```go
type Deployment struct {
    ID         string    `json:"id"`
    App        string    `json:"app"`
    Generation int       `json:"generation"`
    Phase      string    `json:"phase"`
    Desired    AppState  `json:"desired"`
    Previous   AppState  `json:"previous"`
    Status     string    `json:"status"`
}

func (d *Deployment) Advance(phase string) error {
    d.Phase = phase
    return persistDeployment(d)
}
```

Recommended flow:

- Stage code in a new directory.
- Build and health-check the staged deployment.
- Atomically switch the Caddy target.
- Persist registry state.
- Keep the previous generation until the new generation is verified.
- Roll back using `docker compose down` without `-v` unless explicitly requested.
- Mark the operation `partial` if cleanup or metadata persistence fails.

### High: SSH/SCP operations lack context cancellation and retry policy

All remote operations use `exec.Command` (`internal/remote/remote.go:126`, `141`, `154`, `176`, `297`). There is only a 10-second SSH connect timeout. There is no overall operation deadline, context cancellation, keepalive configuration, retry with backoff, resumable upload, or distinction between connection failure and remote command failure.

**Recommended pattern:**

```go
func RunCommand(ctx context.Context, r *Remote, command string) error {
    args := buildSSHArgs(r, command)
    cmd := exec.CommandContext(ctx, "ssh", args...)
    cmd.Stdin = nil
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Run(); err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            return fmt.Errorf("%w: remote exit status %d: %v",
                ErrRemote, exitErr.ExitCode(), err)
        }
        return fmt.Errorf("%w: %v", ErrRemote, err)
    }
    return nil
}
```

Add SSH options such as:

```text
-o ServerAliveInterval=15
-o ServerAliveCountMax=3
-o ConnectionAttempts=3
```

For large artifacts, prefer SFTP, rsync-like transfer, or chunked uploads with checksums.

### High: Blue/green implementation is fragile

`ComposeUpWithPort` modifies YAML with string replacement (`internal/deploy/deploy.go:563-566`). This can alter unrelated strings and fail for YAML quoting variations, multiple services, IPv6 mappings, variables, or custom Compose layouts.

The temporary Compose file is also written under `/tmp` while it references relative paths such as `build: .` and `env_file: .env`. Relative path resolution can point at `/tmp` rather than the application directory.

Use Compose override files in the application directory and an explicit project directory:

```go
override := filepath.Join(dir, ".vitrina", "green.override.yml")
args := []string{
    "compose",
    "--project-directory", dir,
    "-f", composePath,
    "-f", override,
    "--project-name", projectName,
    "up", "-d", "--build",
}
```

Longer-term, parse Compose YAML structurally instead of performing text replacement.

### High: Caddy path handling is inconsistent

`WriteAppConfig` derives paths from configuration, but `Validate` and `Reload` hardcode `/etc/caddy/Caddyfile` (`internal/caddy/caddy.go:65-94`). `WriteMainCaddyfile` also hardcodes the default Caddy directories.

This can validate one file while reloading another.

**Recommended API:**

```go
func Validate(ctx context.Context, mainFile string) error {
    cmd := exec.CommandContext(ctx, "caddy", "validate",
        "--config", mainFile,
        "--adapter", "caddyfile")
    // capture and wrap output
    return cmd.Run()
}
```

Store the main Caddyfile path in `config.Config` and pass it through all operations.

### Medium: Health checks are too weak for deployment safety

`WaitForPort` only checks that TCP accepts a connection (`internal/deploy/deploy.go:657-669`). A process can listen while returning errors, serving the wrong application, or failing migrations.

Use the configured health path and an HTTP client with context:

```go
func WaitHealthy(ctx context.Context, url string) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return err
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode < 200 || resp.StatusCode >= 400 {
        return fmt.Errorf("health check returned %s", resp.Status)
    }
    return nil
}
```

Require health success before Caddy switching and record response status and elapsed time.

### Medium: Git recovery is locale-dependent

`PullLatest` detects divergence by matching English stderr text (`internal/deploy/deploy.go:79-87`). This can fail under non-English Git locales.

Prefer Git state and exit codes:

```go
cmd := exec.Command("git", "-C", dir, "status", "--porcelain", "--branch")
out, err := cmd.Output()
if err != nil {
    return fmt.Errorf("git status: %w", err)
}
```

Use `git fetch`, `rev-parse`, and explicit ancestor checks instead of parsing user-facing error strings.

### Medium: Archive extraction and replacement need stronger safety

`push` creates an archive using external `tar` and extracts it into a live directory. The replacement is not atomic. A failed extraction can leave a partially updated application.

Stage extraction into a temporary directory, validate it, then rename:

```go
stage, err := os.MkdirTemp(filepath.Dir(appDir), ".stage-*")
if err != nil {
    return err
}
defer os.RemoveAll(stage)

if err := extractArchive(archive, stage); err != nil {
    return err
}

old := appDir + ".previous"
_ = os.RemoveAll(old)
if err := os.Rename(appDir, old); err != nil {
    return err
}
if err := os.Rename(stage, appDir); err != nil {
    _ = os.Rename(old, appDir)
    return err
}
```

Preserve `.env` separately and validate archive paths, symlinks, ownership, and permissions.

### Medium: Context propagation is absent throughout the application

`WaitForPort` uses `time.Sleep`, Git commands have no deadlines, Docker commands cannot be cancelled, and MCP request contexts are not passed into subprocesses.

Every long-running operation should accept `context.Context`:

```go
func ComposeUp(ctx context.Context, dir string) error {
    cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--build")
    cmd.Dir = dir
    return cmd.Run()
}
```

This is especially important for MCP, where client cancellation currently does not stop the underlying deployment.

## Prioritized Top 3

1. **Introduce transactional deployments with staged generations, health checks, durable phases, and rollback.** This addresses the largest risk: inconsistent VPS state after partial failure.
2. **Implement a stable machine interface.** Add structured JSON envelopes, stable error codes, deterministic output, proper exit statuses, `--dry-run`, and context-aware MCP execution.
3. **Fix operational security and remote reliability.** Secure `.env` files and backups, redact environment values, add SSH deadlines/retries/keepalives, and replace destructive live-directory updates with staged atomic swaps.
