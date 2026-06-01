# Vitrina — Agent Instructions

## Overview

Vitrina is a lightweight PaaS-lite CLI toolbox (Go) for managing web apps on a single Linux VPS. It uses Caddy as edge router with automatic TLS and per-subdomain reverse proxying.

## Build & Test

```bash
go mod tidy           # sync dependencies
go build -o vitrina . # build binary
go vet ./...          # static analysis
```

Each internal package has `_test.go` files. Run tests with:

```bash
go test ./...
```

The binary lives at `./vitrina` after build.

## Architecture

```
cmd/            Cobra commands — user-facing logic, argument parsing
  init.go       One-time server setup (Caddyfile, config, directories)
  bootstrap.go  Fresh-VPS setup: installs Docker + Caddy, uploads binary, runs init
  config.go     Update global config (domain, email)
  add.go        Register a manually-run app; auto-assigns port if omitted
  remove.go     Deregister an app and remove its Caddy snippet
  list.go       Tabular view of registered apps; optional TCP health check
  deploy.go     Clone a git repo and wire it up end-to-end
  redeploy.go   git pull (auto-recovers from force-push) + docker compose up --build + caddy reload; -q suppresses build output; --force uses fetch+reset
  push.go       Deploy a local directory directly to a remote VPS
  env.go        Manage secure environment variables for an app
  lifecycle.go  Stop, start, and restart an app's containers
  status.go     Show consolidated app metadata and container state
  logs.go       docker compose logs passthrough
  ps.go         docker compose ps for all registered apps
  remote.go     Manage SSH remote profiles; proxy any command over SSH
  doctor.go     Diagnose and heal inconsistencies across registry, Caddy, and Docker
  reload.go     Reload Caddy to pick up config changes and retry TLS certs
  export.go     Backup state to a tarball
  import.go     Restore state from a tarball
  mcp.go        Start MCP server for AI agent integration

internal/
  config/       Reads/writes /etc/vitrina/config.json (domain, email, paths)
  registry/     Thread-safe JSON store for /etc/vitrina/apps.json; supports Add, Remove, Update, Get, List
  caddy/        Snippet management, caddy validate, reload (3 fallback methods), ListAppConfigs
  scaffold/     Boilerplate generators: docker-compose.yml, systemd .service
  deploy/       Git helpers (CloneRepo, PullLatest with force-push recovery, ForcePull), Procfile parser, docker-compose generator, ComposeUp (quiet mode), ComposeRestart, ComposeIsRunning, env helpers
  remote/       SSH remote profile store; Execute, RunScript, RunCommand, UploadFile
  mcp/          MCP server exposing Vitrina commands as tools for AI agents; all handlers shell out to the vitrina binary (no duplicated local logic)
```

**Key principle:** One app = one `.caddy` snippet in `/etc/caddy/conf.d/`. A bad config for one app never breaks the proxy for others.

## Agent Workflow — How to Deploy

Run `vitrina` CLI commands via bash. Use `-r <remote>` to target a remote VPS, or omit for local execution.

### Deploying code changes

You have two paths. Pick one.

**Path A: Push (for local changes, no git required)**
```bash
vitrina -r prod push <subdomain> .
```
Packages the current directory, uploads, extracts, and redeploys. Use this when you've made local changes and want to deploy immediately.

**Path B: Git (for deployed repos)**
```bash
vitrina -r prod redeploy <subdomain> -q
```
Pulls latest from the repo and rebuilds containers. Use this when code is already committed and pushed.

### Deploying a brand-new app
```bash
vitrina -r prod deploy <subdomain> <git_url> --branch main -q
```
Clones, detects Dockerfile/Procfile/docker-compose.yml, generates config, wires up Caddy, and starts containers. `-q` suppresses build output — prints timing summary.

### Setting environment variables
```bash
vitrina -r prod env set <subdomain> --apply
```
Writes to `.env` AND restarts containers immediately. Do NOT chain `env set --apply` + `redeploy` — the restart happens automatically.

### Removing an app
```bash
vitrina -r prod remove <subdomain>      # registry + Caddy only
vitrina -r prod remove <subdomain> -c   # also tear down containers + delete dir
```

### Diagnosing issues
```bash
vitrina -r prod doctor           # report only
vitrina -r prod doctor --heal    # auto-fix
```

### Common antipatterns — do NOT do these

- **DO NOT** SSH into the VPS and manually write files. Use `vitrina push`.
- **DO NOT** call `env set --apply` then `redeploy`. `--apply` already restarts containers.
- **DO NOT** manually edit Caddy configs. Use `vitrina add`/`deploy`/`remove`.
- **DO NOT** chain `git add/commit/push` + `vitrina redeploy` when `vitrina push` works without git.

### Verifying deployment
```bash
vitrina -r prod list --health    # are apps routing and healthy?
vitrina -r prod status <sub>     # detailed status, git info, containers
vitrina -r prod ps               # which containers are running?
vitrina -r prod logs <sub> --tail 20  # check for errors
```

## Filesystem Layout (production)

```
/etc/vitrina/config.json              # {domain, email, caddy_conf_dir, apps_dir}
/etc/vitrina/apps.json                # {"apps": {"sub": {subdomain, fqdn, port, created_at, scaffold, git_url, git_ref, last_deployed_at, env_keys}}}
/etc/vitrina/apps/<subdomain>/        # Cloned repo + generated docker-compose.yml / .env
/etc/vitrina/apps/<subdomain>/.vitrina.json  # Marker file: subdomain, fqdn, port, managed_by, deployed_by
/etc/caddy/Caddyfile                  # Main config, imports conf.d/*
/etc/caddy/conf.d/<fqdn>.caddy        # One reverse_proxy block per app
```

## CLI Commands

| Command | Args | Flags | Notes |
|---------|------|-------|-------|
| `bootstrap` | `<remote>` | `--domain`, `--email`, `--binary` | Installs deps, uploads binary, runs init; auto-builds Linux binary if `--binary` omitted and not on Linux |
| `init` | — | `-d/--domain`, `-e/--email` (required) | Must run once before any other command |
| `add` | `<subdomain> [port]` | `-s/--scaffold` (docker\|systemd\|none) | Port auto-assigned from 3000 if omitted |
| `remove` | `<subdomain>` | `-c/--clean` | Removes snippet + registry entry |
| `list` | — | `--health` (TCP + HTTP), `--public` (HTTPS with TLS cert days) | Tabwriter-formatted table |
| `deploy` | `<subdomain> <git-url>` | `--branch`, `--tag`, `-q/--quiet`, `-z/--zero-downtime` | Clone → compose → Caddy → up; `-q` suppresses build output, prints timing summary; `-z` does blue/green swap |
| `redeploy` | `<subdomain>` | `--branch`, `--tag`, `-q/--quiet`, `--force`, `-z/--zero-downtime` | Pull/checkout → docker compose up --build → caddy reload; `--force` uses fetch+reset (also auto-triggered on divergence); `-z` does blue/green swap |
| `push` | `<subdomain> [local_dir]` | — | Package and deploy a local directory directly |
| `env` | `list\|set\|unset <subdomain>` | `set/unset: --apply` | Manage secure environment variables; `--apply` calls `ComposeRestart` immediately after |
| `status` | `<subdomain>` | `--json` | Show consolidated app metadata and container state |
| `stop/start/restart` | `<subdomain>` | — | Manage app container lifecycle without rebuilding |
| `logs` | `<subdomain> [service...]` | `-f`, `--tail`, `--since`, `-a/--all` | docker compose logs passthrough; `--all` streams from all apps |
| `ps` | — | `--json` | docker compose ps per registered app |
| `monitor` | — | `-i/--interval`, `--once` | Background daemon for TLS cert expiry, HTTPS health, container liveness |
| `remote` | `set\|list\|show\|default\|remove` | | Manage remote VPS connections |
| `doctor` | — | `--heal` | Diagnose inconsistencies; `--heal` auto-fixes |
| `reload` | — | — | Reload Caddy (retry TLS certs, pick up DNS changes) |
| `config update` | — | `--domain`, `--email` | Modify `/etc/vitrina/config.json` |
| `export` | `[output.tar.gz]` | — | Archive registry, configs, and envs |
| `import` | `<input.tar.gz>` | — | Restore state from archive |
| `upgrade` | — | `--binary` | Rebuild from source and replace local or remote vitrina binary |
| `mcp` | — | — | Start MCP server for AI agent integration |

## Doctor: Diagnostics & Self-Healing

`vitrina doctor` cross-references the registry, Caddy configs, app directories, and Docker containers for inconsistencies. Without `--heal`, it reports problems. With `--heal`, it takes non-destructive corrective action:

1. **Caddy configs vs. registry** — Missing snippets are regenerated via `caddy.WriteAppConfig`. Dangling `.caddy` files (no matching registry entry) are deleted.
2. **App directories vs. registry** — Missing app directories are re-cloned if `GitURL` is set (checks out `GitRef`, generates compose, writes `.env`, runs `ComposeUp`). Stopped containers are restarted via `ComposeUp`.
3. **Dangling app directories** — Directories with no matching registry entry are logged but intentionally NOT deleted (safety precaution).

After healing, Caddy is validated and reloaded. Works with `-r` for remote execution.

## App Metadata Tracking

The registry `App` struct tracks deployment metadata beyond the basics:

| Field | Set by | Purpose |
|-------|--------|---------|
| `GitURL` | `deploy` | Re-clone on `doctor --heal` or future `redeploy` |
| `GitRef` | `deploy`, `redeploy` | Branch or tag currently deployed (`--tag` overrides `--branch`) |
| `LastDeployedAt` | `deploy`, `redeploy` | Timestamp of last successful deployment |
| `EnvKeys` | `env set`, `env unset` | Snapshot of managed env var names (excludes `PORT`) |
| `HealthPath` | — | Reserved for future HTTP health check support |

`registry.Update(app)` allows in-place mutation of existing apps (used by `redeploy`, `env set`/`unset`). Previously, changing metadata required `remove` + `add`.

## Vitrina Marker File

When `deploy` or `push` runs, it writes `/etc/vitrina/apps/<subdomain>/.vitrina.json` to the app directory. This file marks the project as Vitrina-managed and provides metadata for tooling (AI agents, IDEs) to discover the deployment context without querying the registry:

```json
{
  "subdomain": "myapp",
  "fqdn": "myapp.example.com",
  "port": 3000,
  "managed_by": "vitrina",
  "deployed_by": "deploy",
  "git_url": "https://github.com/user/myapp",
  "git_ref": "main"
}
```

The `push` command excludes `.vitrina.json` from the tarball (it would overwrite the server-side marker). The server-side marker is always authoritative.

`deploy` checks in this order and uses the first match. It also records `GitURL`, `GitRef`, and `LastDeployedAt` in the registry:

1. **Repo has `docker-compose.yml`** — used as-is; writes `.env` with `PORT=<assigned>`
2. **Repo has `Procfile`** — generates multi-service compose:
   - `web` → gets port mapping + Caddy route
   - `release` → one-shot (restart: no); `web` depends on it via `service_completed_successfully`
   - everything else → internal only, no port
3. **Repo has `Dockerfile`** — generates single-service compose with `build: .`
4. **None of the above** — hard error; user must add a `Dockerfile` or use `vitrina add`

Port contract: `PORT=<assigned>` is set in the container environment and mapped `127.0.0.1:<port>:<port>`. Apps read `$PORT`.

## Auto-Port Assignment

`registry.NextFreePort(startAt)` scans all registered apps and returns the lowest unused port ≥ startAt (skipping 80/443). `add` and `deploy` both use this when no explicit port is given, starting at 3000.

## Safety / Error Handling

- `add` and `deploy` write the Caddy snippet, validate with `caddy validate`, and only commit to the registry if validation passes. On any failure after that point, changes are rolled back in reverse order.
- `remove` validates after snippet deletion; restores snippet if the resulting config is broken.
- `caddy reload` has 3 fallbacks: `caddy reload`, `systemctl reload caddy`, `pkill -USR1 caddy`.
- Subdomain validated via RFC 1035 regex. Ports 80/443 are rejected.
- Duplicate subdomain and port collisions detected against the registry.
- All mutating commands require root (`os.Geteuid() == 0`).

## Bootstrap

`bootstrap` runs entirely from the local machine — it does not use `runRemoteOrLocal`. Steps:

1. Load remote profile by name; resolve domain/email from flags → remote config → error
2. Resolve binary:
   - `--binary` flag → use as-is
   - Not on Linux → `autoBuildLinuxBinary`: find `go.mod` by walking up from cwd, cross-compile via `go build` with `GOOS=linux GOARCH=amd64` to a temp file, clean up after upload; falls back to error with manual instructions if `go` is not in PATH or no `go.mod` found
   - On Linux → `os.Executable()`
3. `remote.RunScript(r, installScript)` — pipes the embedded shell script to `bash -s` over SSH
4. `remote.UploadFile(r, binaryPath, "/tmp/vitrina")` — SCP
5. `remote.RunCommand(r, "install -m 0755 /tmp/vitrina <VitrinaPath>")` — move binary into place
6. `remote.RunCommand(r, "<VitrinaPath> init --domain ... --email ...")` — initialize

The install script (embedded in `cmd/bootstrap.go`) is idempotent: it checks for existing `docker`/`caddy` binaries before installing. Supports apt (Ubuntu/Debian) and yum (CentOS/RHEL).

`Remote` struct now has `Domain` and `Email` fields stored in `~/.vitrina/remotes.json`, set via `remote set --domain` and `--email`. `remote show` displays them when set. A default remote can be configured with `remote default <name>`, which is automatically used when `-r` is omitted.

The remote package exposes three SSH helpers (all apply sudo when `UseSudo && User != "root"`):
- `RunScript(r, script)` — pipe script to `bash -s`
- `RunCommand(r, command)` — run a single command
- `UploadFile(r, localPath, remotePath)` — SCP upload

## Conventions

- Go stdlib preferred over external dependencies. Only `cobra`, `pflag`, and `mcp-go` are imported.
- Errors are returned, not panic'd. Commands use `RunE` and write to `cmd.ErrOrStderr()` for non-fatal warnings.
- JSON files use `json.MarshalIndent` with 2-space indent.
- File permissions: dirs 0755, files 0644.
- `subdomainRegex` and `requireRoot()` are defined once in the `cmd` package and shared across all command files.

## Areas of Growth

### 1. Hardcoded paths in caddy validate/reload

`caddy.Validate()` and `caddy.Reload()` hardcode `/etc/caddy/Caddyfile` while `caddy.WriteAppConfig()` and `caddy.WriteMainCaddyfile()` derive paths from `*config.Config`. If `Config.CaddyConfDir` ever changes, validate/reload will silently use the wrong path. Fix: pass `*config.Config` (or the main Caddyfile path) into `Validate()` and `Reload()` so all paths are derived from a single source of truth.

### 2. Shared helpers lack a home

`subdomainRegex` and `requireRoot()` are defined ad-hoc in command files and shared implicitly across `cmd/`. As more commands are added, this pattern scatters validation logic. Consider a `cmd/validate.go` or `internal/cli/` package that collects shared CLI helpers (regexes, root checks, flag parsers) in one place.

### 3. No structured logging or verbosity control

All output uses `fmt.Printf` / `fmt.Fprintf`. There is no `-v/--verbose` flag and no log levels. For debugging production issues on a remote VPS, a simple `log`-based approach with verbosity levels would make `vitrina logs` and error tracing far more useful. Consider a lightweight `internal/log` package wrapping `log.Logger` with level filtering.

### 4. Error context in remote operations

`remote.RunCommand` and `remote.RunScript` return raw command exit codes but limited context about which step failed. Enriching remote errors with the SSH command that failed, the remote host, and truncated stderr would make debugging bootstrap/deploy failures significantly easier.

### 5. Git error auto-recovery is locale-dependent

`deploy.PullLatest` matches English error strings (`"diverged"`, `"untracked"`, etc.) to trigger the `forcePull` fallback. A Git configured with a non-English locale will emit different messages and the automatic recovery silently fails. Fix: use exit codes or check `git status --porcelain` instead of parsing stderr strings.

### 6. No `--json` output on mutating commands

Only `list`, `ps`, `status`, and `env list` support the `--json` flag. The MCP server currently passes `--json` to these 4 commands; the other 10 mutating/informational commands lack structured output. Adding `--json` to `add`, `deploy`, `redeploy`, `remove`, `env set`, `env unset`, lifecycle, `doctor`, and `config health-path` would give MCP clients structured success/failure information.
