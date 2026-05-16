# Vitrina — Agent Instructions

## Overview

Vitrina is a lightweight PaaS-lite CLI toolbox (Go) for managing web apps on a single Linux VPS. It uses Caddy as edge router with automatic TLS and per-subdomain reverse proxying.

## Build & Test

```bash
go mod tidy          # sync dependencies
go build -o vitrina . # build binary
go vet ./...          # static analysis
```

There are no tests yet. The binary lives at `./vitrina` after build.

## Architecture

```
cmd/            Cobra commands (init, add, remove, list) — user-facing logic, argument parsing
internal/
  config/       Reads/writes /etc/vitrina/config.json (domain, email, paths)
  registry/     Thread-safe JSON store for /etc/vitrina/apps.json (subdomain→port mappings)
  caddy/        Caddyfile snippet management, validation (caddy validate), reload (3 fallback methods)
  scaffold/     Boilerplate generators: docker-compose.yml, systemd .service
```

**Key principle:** One app = one `.caddy` snippet in `/etc/caddy/conf.d/`. A bad config for one app never breaks the proxy for others.

## Filesystem Layout (production)

```
/etc/vitrina/config.json        # {domain, email, caddy_conf_dir, apps_dir}
/etc/vitrina/apps.json          # {"apps": {"sub": {"subdomain","fqdn","port","created_at"}}}
/etc/vitrina/apps/<subdomain>/  # Boilerplate files (docker-compose.yml, *.service)
/etc/caddy/Caddyfile            # Main config, imports conf.d/*
/etc/caddy/conf.d/*.caddy       # One server block per app
```

## CLI Commands

| Command | Args | Flags | Notes |
|---------|------|-------|-------|
| `init` | — | `-d/--domain`, `-e/--email` (required) | Must run once before any other command |
| `add` | `<subdomain> <port>` | `-s/--scaffold` (docker\|systemd\|none) | Validates Caddy, writes snippet, reloads |
| `remove` | `<subdomain>` | `-c/--clean` | Removes snippet + registry entry |
| `list` | — | `--health` (TCP dial check) | Tabwriter-formatted table |

## Safety / Error Handling

- `add` writes the Caddy snippet, validates, and only commits to the registry if validation passes. On failure, the snippet is rolled back.
- `remove` validates after snippet deletion. If config breaks, the snippet is restored.
- `caddy reload` has 3 fallbacks: `caddy reload`, `systemctl reload caddy`, `pkill -USR1 caddy`.
- Subdomain validated via RFC 1035 regex. Ports 80/443 are rejected.
- Duplicate subdomain and port collisions are detected against the registry.
- All mutating commands (`add`, `remove`, `init`) require root (`os.Geteuid() == 0`).

## Conventions

- Go stdlib preferred over external dependencies. Only `cobra` + `pflag` are imported.
- Tab size: tabs (go fmt default).
- Errors are returned, not panic'd. Commands use `RunE` and write to `cmd.ErrOrStderr()` for warnings.
- JSON files use `json.MarshalIndent` with 2-space indent.
- File permissions: dirs 0755, files 0644.
