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
  redeploy.go   git pull + docker compose up --build
  push.go       Deploy a local directory directly to a remote VPS
  env.go        Manage secure environment variables for an app
  lifecycle.go  Stop, start, and restart an app's containers
  status.go     Show consolidated app metadata and container state
  logs.go       docker compose logs passthrough
  ps.go         docker compose ps for all registered apps
  remote.go     Manage SSH remote profiles; proxy any command over SSH
  doctor.go     Diagnose and heal inconsistencies across registry, Caddy, and Docker
  export.go     Backup state to a tarball
  import.go     Restore state from a tarball

internal/
  config/       Reads/writes /etc/vitrina/config.json (domain, email, paths)
  registry/     Thread-safe JSON store for /etc/vitrina/apps.json; supports Add, Remove, Update, Get, List
  caddy/        Snippet management, caddy validate, reload (3 fallback methods), ListAppConfigs
  scaffold/     Boilerplate generators: docker-compose.yml, systemd .service
  deploy/       Git helpers, Procfile parser, docker-compose generator, compose wrappers, ComposeIsRunning, env helpers
  remote/       SSH remote profile store; Execute, RunScript, RunCommand, UploadFile
```

**Key principle:** One app = one `.caddy` snippet in `/etc/caddy/conf.d/`. A bad config for one app never breaks the proxy for others.

## Filesystem Layout (production)

```
/etc/vitrina/config.json              # {domain, email, caddy_conf_dir, apps_dir}
/etc/vitrina/apps.json                # {"apps": {"sub": {subdomain, fqdn, port, created_at, scaffold, git_url, git_ref, last_deployed_at, env_keys}}}
/etc/vitrina/apps/<subdomain>/        # Cloned repo + generated docker-compose.yml / .env
/etc/caddy/Caddyfile                  # Main config, imports conf.d/*
/etc/caddy/conf.d/<fqdn>.caddy        # One reverse_proxy block per app
```

## CLI Commands

| Command | Args | Flags | Notes |
|---------|------|-------|-------|
| `bootstrap` | `<remote>` | `--domain`, `--email`, `--binary` | Installs deps, uploads binary, runs init on remote VPS |
| `init` | — | `-d/--domain`, `-e/--email` (required) | Must run once before any other command |
| `add` | `<subdomain> [port]` | `-s/--scaffold` (docker\|systemd\|none) | Port auto-assigned from 3000 if omitted |
| `remove` | `<subdomain>` | `-c/--clean` | Removes snippet + registry entry |
| `list` | — | `--health` (TCP dial check) | Tabwriter-formatted table |
| `deploy` | `<subdomain> <git-url>` | `--branch`, `--tag` | Clone → compose → Caddy → up |
| `redeploy` | `<subdomain>` | `--branch`, `--tag` | Pull/checkout → docker compose up --build |
| `push` | `<subdomain> [local_dir]` | — | Package and deploy a local directory directly |
| `env` | `list\|set\|unset <subdomain>` | — | Manage secure environment variables |
| `status` | `<subdomain>` | `--json` | Show consolidated app metadata and container state |
| `stop/start/restart` | `<subdomain>` | — | Manage app container lifecycle without rebuilding |
| `logs` | `<subdomain>` | `-f`, `--tail`, `--since` | Passes through to docker compose logs |
| `ps` | — | `--json` | docker compose ps per registered app |
| `remote` | `set\|list\|show\|default\|remove` | | Manage remote VPS connections |
| `doctor` | — | `--heal` | Diagnose inconsistencies; `--heal` auto-fixes |
| `config update` | — | `--domain`, `--email` | Modify `/etc/vitrina/config.json` |
| `export` | `[output.tar.gz]` | — | Archive registry, configs, and envs |
| `import` | `<input.tar.gz>` | — | Restore state from archive |

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
2. Resolve binary: `--binary` flag → current executable (Linux only) → error with cross-compile instructions
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

- Go stdlib preferred over external dependencies. Only `cobra` + `pflag` are imported.
- Errors are returned, not panic'd. Commands use `RunE` and write to `cmd.ErrOrStderr()` for non-fatal warnings.
- JSON files use `json.MarshalIndent` with 2-space indent.
- File permissions: dirs 0755, files 0644.
- `subdomainRegex` and `requireRoot()` are defined once in the `cmd` package and shared across all command files.

## Areas of Growth

### 1. Inconsistent mutex usage in registry

`registry.Add()` and `registry.Remove()` acquire `sync.Mutex`, but `Get()`, `List()`, and `NextFreePort()` do not. This is benign for a single-user CLI but architecturally inconsistent. If the codebase ever runs commands concurrently or the registry is reused as a library, these reads will race with writes. Fix: acquire the mutex in all public methods, or document the single-goroutine contract.

### 2. Hardcoded paths in caddy validate/reload

`caddy.Validate()` and `caddy.Reload()` hardcode `/etc/caddy/Caddyfile` while `caddy.WriteAppConfig()` and `caddy.WriteMainCaddyfile()` derive paths from `*config.Config`. If `Config.CaddyConfDir` ever changes, validate/reload will silently use the wrong path. Fix: pass `*config.Config` (or the main Caddyfile path) into `Validate()` and `Reload()` so all paths are derived from a single source of truth.

### 3. Shared helpers lack a home

`subdomainRegex` and `requireRoot()` are defined ad-hoc in command files and shared implicitly across `cmd/`. As more commands are added, this pattern scatters validation logic. Consider a `cmd/validate.go` or `internal/cli/` package that collects shared CLI helpers (regexes, root checks, flag parsers) in one place.

### 4. No structured logging or verbosity control

All output uses `fmt.Printf` / `fmt.Fprintf`. There is no `-v/--verbose` flag and no log levels. For debugging production issues on a remote VPS, a simple `log`-based approach with verbosity levels would make `vitrina logs` and error tracing far more useful. Consider a lightweight `internal/log` package wrapping `log.Logger` with level filtering.

### 5. No integration or end-to-end tests

Unit tests exist for each internal package, but there are no integration tests that exercise the full CLI flow (e.g., `add` → `list` → `remove`). Given that filesystem and Caddy state interact, a test harness that runs commands against a temp directory (via `--config-dir` flag or environment variable) would catch cross-package regressions.

### 6. Error context in remote operations

`remote.RunCommand` and `remote.RunScript` return raw command exit codes but limited context about which step failed. Enriching remote errors with the SSH command that failed, the remote host, and truncated stderr would make debugging bootstrap/deploy failures significantly easier.
