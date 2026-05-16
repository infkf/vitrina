# Vitrina — Agent Instructions

## Overview

Vitrina is a lightweight PaaS-lite CLI toolbox (Go) for managing web apps on a single Linux VPS. It uses Caddy as edge router with automatic TLS and per-subdomain reverse proxying.

## Build & Test

```bash
go mod tidy           # sync dependencies
go build -o vitrina . # build binary
go vet ./...          # static analysis
```

There are no tests yet. The binary lives at `./vitrina` after build.

## Architecture

```
cmd/            Cobra commands — user-facing logic, argument parsing
  init.go       One-time server setup (Caddyfile, config, directories)
  bootstrap.go  Fresh-VPS setup: installs Docker + Caddy, uploads binary, runs init
  add.go        Register a manually-run app; auto-assigns port if omitted
  remove.go     Deregister an app and remove its Caddy snippet
  list.go       Tabular view of registered apps; optional TCP health check
  deploy.go     Clone a git repo and wire it up end-to-end
  redeploy.go   git pull + docker compose up --build
  push.go       Deploy a local directory directly to a remote VPS
  env.go        Manage secure environment variables for an app
  logs.go       docker compose logs passthrough
  ps.go         docker compose ps for all registered apps
  remote.go     Manage SSH remote profiles; proxy any command over SSH

internal/
  config/       Reads/writes /etc/vitrina/config.json (domain, email, paths)
  registry/     Thread-safe JSON store for /etc/vitrina/apps.json
  caddy/        Snippet management, caddy validate, reload (3 fallback methods)
  scaffold/     Boilerplate generators: docker-compose.yml, systemd .service
  deploy/       Git helpers, Procfile parser, docker-compose generator, compose wrappers
  remote/       SSH remote profile store; Execute, RunScript, RunCommand, UploadFile
```

**Key principle:** One app = one `.caddy` snippet in `/etc/caddy/conf.d/`. A bad config for one app never breaks the proxy for others.

## Filesystem Layout (production)

```
/etc/vitrina/config.json              # {domain, email, caddy_conf_dir, apps_dir}
/etc/vitrina/apps.json                # {"apps": {"sub": {subdomain, fqdn, port, created_at, scaffold}}}
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
| `logs` | `<subdomain>` | `-f/--follow` (default true) | Passes through to docker compose logs |
| `ps` | — | — | docker compose ps per registered app |
| `remote` | `set\|list\|show\|default\|remove` | | Manage remote VPS connections |

## Deploy: Source Detection

`deploy` checks in this order and uses the first match:

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
