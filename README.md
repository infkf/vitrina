# Vitrina

Lightweight PaaS-lite CLI toolbox for managing web apps on a single Linux VPS. Uses Caddy as edge router with automatic TLS and per-subdomain reverse proxying.

## Quick Start

```bash
# Build a Linux binary (required if you're on macOS/Windows)
GOOS=linux GOARCH=amd64 go build -o vitrina-linux .

# Point at your VPS and bootstrap it in one command
vitrina remote set prod --host 1.2.3.5 --domain mydomain.com --email admin@mydomain.com
vitrina bootstrap prod --binary ./vitrina-linux

# Deploy an app from a git repository
vitrina -r prod deploy api https://github.com/user/api
# → https://api.mydomain.com is live

# Or register a manually-run app
vitrina -r prod add myapp        # auto-assigns next free port
vitrina -r prod add myapp 4000   # explicit port

# Redeploy after a push
vitrina -r prod redeploy api
vitrina -r prod redeploy api --branch staging
vitrina -r prod redeploy api --tag v1.2.3

# Push a local directory directly
vitrina -r prod push myapp .

# Set environment variables securely
vitrina -r prod env set myapp DATABASE_URL=postgres://...

# Set a default remote so you never have to type -r prod again
vitrina remote default prod
vitrina logs api

# Observe
vitrina ps
vitrina list --health
vitrina status api
```

## Commands

| Command | Args | Flags | Description |
|---------|------|-------|-------------|
| `bootstrap` | `<remote>` | `--domain`, `--email`, `--binary` | Install Docker, Caddy, and Vitrina on a fresh VPS |
| `init` | — | `-d`, `-e` | Initialize Vitrina on this server (called by bootstrap) |
| `add` | `<subdomain> [port]` | `-s` (docker\|systemd\|none) | Register an app; auto-assigns port if omitted |
| `deploy` | `<subdomain> <git-url>` | `--branch`, `--tag` | Clone, containerize, and route an app |
| `redeploy` | `<subdomain>` | `--branch`, `--tag` | Pull latest and rebuild containers |
| `push` | `<subdomain> [local_dir]` | — | Package and deploy a local directory directly |
| `env` | `list\|set\|unset <subdomain>` | — | Manage environment variables |
| `status` | `<subdomain>` | `--json` | Show consolidated app details and container status |
| `stop` | `<subdomain>` | — | Pause an app's containers |
| `start` | `<subdomain>` | — | Resume an app's containers |
| `restart` | `<subdomain>` | — | Restart an app's containers |
| `logs` | `<subdomain>` | `-f`, `--tail`, `--since` | Stream container logs; filter by service |
| `ps` | — | `--json` | Show container status for all apps |
| `remove` | `<subdomain>` | `-c` | Remove an app from the proxy |
| `list` | — | `--health`, `--json` | Show all registered apps; `--health` does HTTP then TCP checks |
| `remote` | `set\|list\|show\|default\|remove` | | Manage remote VPS connections |
| `doctor` | — | `--heal` | Diagnose and optionally heal ecosystem inconsistencies |
| `config update` | — | `--domain`, `--email` | Update global configuration |
| `export` | `[output.tar.gz]` | — | Export Vitrina state to a backup archive |
| `import` | `<input.tar.gz>` | — | Restore state from a backup archive; runs `doctor --heal` |
| `mcp` | — | — | Start MCP server for AI agent integration |

## Bootstrap

`vitrina bootstrap` preps a bare VPS in one command — no manual SSH required.

```bash
# Store domain and email in the remote profile once
vitrina remote set prod --host 1.2.3.5 --domain example.com --email me@example.com

# If you're on macOS/Windows, cross-compile first
GOOS=linux GOARCH=amd64 go build -o vitrina-linux .

vitrina bootstrap prod --binary ./vitrina-linux
# → installs Docker (apt/yum auto-detected)
# → installs Caddy via official package repo
# → uploads vitrina binary to /usr/local/bin/vitrina
# → runs vitrina init --domain example.com --email me@example.com
```

The script is idempotent — safe to re-run if something fails partway through.

## Remote Execution

After bootstrap, run any command on the remote with `-r <name>`:

```bash
vitrina -r prod deploy api https://github.com/user/api
vitrina -r prod logs api --tail 50
vitrina -r prod ps
vitrina -r prod list --health
```

If you only use one server, configure it as your default so you don't have to provide the flag every time:

```bash
vitrina remote default prod
vitrina push api .  # runs on prod
```

## Deploy: How It Works

`vitrina deploy` goes from git URL to live HTTPS in one step. Port is assigned automatically.

Source detection (first match wins):

| Found in repo | Behaviour |
|---|---|
| `docker-compose.yml` | Used as-is; `PORT=<assigned>` injected via `.env` |
| `Procfile` | Multi-service compose generated; only `web` gets a Caddy route |
| `Dockerfile` | Single-service compose generated |
| None of the above | Error — add a `Dockerfile` or use `vitrina add` |

**Procfile example** (Rails + Sidekiq):

```
web: bundle exec rails server -p $PORT
worker: bundle exec sidekiq
release: bundle exec rails db:migrate
```

`release` runs once and `web` waits for it before starting. Workers run internally with no port mapping.

**Port contract:** vitrina sets `PORT=<assigned>` in the container environment and maps it `127.0.0.1:<port>:<port>`. Apps just need to read `$PORT` — Express, Flask, Rails, and most frameworks already do this by default.

## Doctor: Diagnostics & Self-Healing

`vitrina doctor` cross-references the registry, Caddy configs, app directories, and Docker containers. Without flags it reports problems; with `--heal` it takes non-destructive corrective action:

- Missing Caddy snippets are regenerated
- Dangling `.caddy` files (no matching registry entry) are deleted
- Missing app directories are re-cloned from the stored `GitURL` (if available)
- Stopped containers are restarted
- Dangling app directories are logged but **not** deleted (safety precaution)

After healing, Caddy is validated and reloaded.

## Backup & Restore

```bash
# Export current state (registry, Caddy configs, .env files)
vitrina export vitrina-backup.tar.gz

# Import on a fresh server — restores state and runs doctor --heal
vitrina import vitrina-backup.tar.gz
```

Works with `-r` for remote operations: export from a remote, import to a new one.

## App Metadata

The registry tracks more than just subdomains and ports:

| Field | Set by | Purpose |
|-------|--------|---------|
| `GitURL` | `deploy` | Re-clone on `doctor --heal` |
| `GitRef` | `deploy`, `redeploy` | Branch or tag currently deployed |
| `LastDeployedAt` | `deploy`, `redeploy` | Timestamp of last successful deployment |
| `EnvKeys` | `env set`, `env unset` | Snapshot of managed env var names |

This metadata powers `doctor --heal` (re-cloning missing apps), `status` (showing deployment info), and `redeploy` (knowing which branch/tag to pull).

## MCP Server

`vitrina mcp` starts a Model Context Protocol server over stdio, exposing all Vitrina operations as tools for AI agents. Works with Claude, Cursor, opencode, and other MCP clients.

```json
// Add to your MCP client configuration:
{
  "mcpServers": {
    "vitrina": {
      "command": "sudo",
      "args": ["vitrina", "mcp"]
    }
  }
}
```

Exposed tools: `list_apps`, `get_app`, `add_app`, `deploy_app`, `redeploy_app`, `remove_app`, `stop_app`, `start_app`, `restart_app`, `env_list`, `env_set`, `env_unset`, `app_logs`, `ps_apps`, `doctor`.

All mutating operations require root — configure MCP clients to run `vitrina mcp` via `sudo`.

## How It Works

- One Caddy snippet per app in `/etc/caddy/conf.d/`
- Snippets are validated before any changes take effect; bad config for one app never breaks others
- Caddy reload supports three fallback methods
- App registry stored in `/etc/vitrina/apps.json`
- Deployed apps cloned to `/etc/vitrina/apps/<subdomain>/`

## Filesystem Layout

```
/etc/vitrina/config.json              # {domain, email, caddy_conf_dir, apps_dir}
/etc/vitrina/apps.json                # Registry with metadata per app
/etc/vitrina/apps/<subdomain>/        # Cloned repo + generated docker-compose.yml / .env
/etc/caddy/Caddyfile                  # Main config, imports conf.d/*
/etc/caddy/conf.d/<fqdn>.caddy        # One reverse_proxy block per app
```

## Build & Test

```bash
go mod tidy           # sync dependencies
go build -o vitrina . # build binary
go vet ./...          # static analysis
go test ./...         # run tests
```

The binary lives at `./vitrina` after build.

## Safety

- `add` and `deploy` write Caddy config first, validate, then commit to the registry. Failures are rolled back in reverse order.
- `remove` validates after snippet deletion; restores the snippet if the resulting config is broken.
- Subdomains are validated against RFC 1035. Ports 80/443 are rejected. Duplicate subdomains and port collisions are caught.
- All mutating commands require root (`os.Geteuid() == 0`).