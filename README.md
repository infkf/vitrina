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

# Observe
vitrina -r prod logs api
vitrina -r prod ps
vitrina -r prod list
```

## Commands

| Command | Args | Flags | Description |
|---------|------|-------|-------------|
| `bootstrap` | `<remote>` | `--domain`, `--email`, `--binary` | Install Docker, Caddy, and Vitrina on a fresh VPS |
| `init` | — | `-d`, `-e` | Initialize Vitrina on this server (called by bootstrap) |
| `add` | `<subdomain> [port]` | `-s` (docker\|systemd\|none) | Register an app; auto-assigns port if omitted |
| `deploy` | `<subdomain> <git-url>` | `--branch`, `--tag` | Clone, containerize, and route an app |
| `redeploy` | `<subdomain>` | `--branch`, `--tag` | Pull latest and rebuild containers |
| `logs` | `<subdomain>` | `-f/--follow` | Stream container logs |
| `ps` | — | — | Show container status for all apps |
| `remove` | `<subdomain>` | `-c` | Remove an app from the proxy |
| `list` | — | `--health` | Show all registered apps and their proxy status |
| `remote` | `set\|list\|show\|default\|remove` | | Manage remote VPS connections |

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
vitrina -r prod logs api
vitrina -r prod ps
vitrina -r prod list
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

## How It Works

- One Caddy snippet per app in `/etc/caddy/conf.d/`
- Snippets are validated before any changes take effect; bad config for one app never breaks others
- Caddy reload supports three fallback methods
- App registry stored in `/etc/vitrina/apps.json`
- Deployed apps cloned to `/etc/vitrina/apps/<subdomain>/`
