# Vitrina Onboarding Roadmap

Goal: reduce the steps from "fresh VPS" to "app live at HTTPS subdomain" to as few commands as possible.

Current flow (5+ steps, manual port tracking):
```
vitrina remote set prod --host 1.2.3.5
# SSH in, install Docker, Caddy, vitrina manually
vitrina -r prod init --domain example.com --email me@example.com
vitrina -r prod add api 3000 -s docker
# SSH in, git clone, configure PORT, run docker compose up -d
```

Target flow (3 commands, zero manual SSH):
```
vitrina remote set prod --host 1.2.3.5
vitrina bootstrap prod
vitrina -r prod deploy api https://github.com/user/api
# → https://api.example.com is live
```

---

## Phase 1 — Friction reducers (small, independent, mergeable now)

### 1.1 Auto-port assignment

Make `<port>` optional in `add`. When omitted, scan the registry for used ports and pick the lowest free one starting at 3000.

```
vitrina add api          # auto-assigns 3000
vitrina add web          # auto-assigns 3001
vitrina add metrics 9090 # explicit port still works
```

The registry already detects port conflicts — this is a matter of adding a `nextFreePort()` helper that walks the existing apps and finds the gap.

### 1.2 Implicit default remote

When a default remote is configured and `-r` is not passed, use it automatically instead of running locally. Users with a single VPS never need to type `-r prod` again.

```
vitrina remote default prod
vitrina deploy api https://...  # runs on prod, no -r needed
```

`cmd/root.go` already parses `-r`; this adds a fallback to `cfg.Default` when the flag is empty.

---

## Phase 2 — Deploy command

### 2.1 `vitrina deploy <subdomain> <git-url>`

One command from GitHub URL to live HTTPS endpoint. Runs entirely on the VPS side.

```
vitrina -r prod deploy api https://github.com/user/api
# → clones to /etc/vitrina/apps/api
# → auto-assigns port (Phase 1.1)
# → writes docker-compose.yml with PORT env var
# → docker compose up -d --build
# → writes Caddy snippet, reloads Caddy
# → https://api.example.com is live
```

Port contract: vitrina injects `PORT=<assigned>` into the container and maps it `127.0.0.1:<port>:<port>`. Apps read `$PORT` — Express, Flask, Rails, Go's `net/http`, and most other frameworks already do this by default.

Generated `docker-compose.yml`:
```yaml
services:
  api:
    build: .
    restart: unless-stopped
    environment:
      - PORT=3000
    ports:
      - "127.0.0.1:3000:3000"
```

Companion commands:
```
vitrina logs api          # docker compose logs -f
vitrina redeploy api      # git pull + rebuild + restart
vitrina ps                # container status alongside routing table
```

`redeploy` also accepts `--branch` and `--tag`:
```
vitrina redeploy api --branch staging
vitrina redeploy api --tag v1.2.3
```

### 2.2 Multi-service apps (Procfile)

When the repo contains a `Procfile`, vitrina generates a multi-container compose file. Only the `web` process gets a port mapping and Caddy route. Workers are internal-only.

```
# Procfile in repo root:
web: bundle exec rails server -p $PORT
worker: bundle exec sidekiq
release: bundle exec rails db:migrate
```

Generated compose:
```yaml
services:
  api-web:
    build: .
    command: ["bundle", "exec", "rails", "server"]
    environment:
      - PORT=3000
    ports:
      - "127.0.0.1:3000:3000"
  api-worker:
    build: .
    command: ["bundle", "exec", "sidekiq"]
    # no ports — internal only
```

If the repo ships its own `docker-compose.yml`, vitrina uses it as-is, only injecting `PORT` into the `web` service and adding the Caddy snippet.

Priority: repo's `docker-compose.yml` → `Procfile` → single-container fallback.

---

## Phase 3 — Bootstrap

### 3.1 `vitrina bootstrap <remote>`

Installs all prerequisites on a bare VPS over SSH. Run once per machine before any `deploy`.

```
vitrina bootstrap prod
# → installs Docker (apt/yum auto-detected)
# → installs Caddy via official apt repo
# → uploads vitrina binary
# → runs vitrina init --domain <from remote config> --email <from remote config>
```

Domain and email can be stored in the remote profile at `remote set` time:
```
vitrina remote set prod --host 1.2.3.5 --domain example.com --email me@example.com
vitrina bootstrap prod   # uses stored domain + email, no extra flags
```

Bootstrap uploads and runs a shell script over SSH. No configuration management or external dependencies needed.

---

## Summary

| Phase | Command | Unlocks |
|-------|---------|---------|
| 1.1 | auto-port in `add` | no manual port tracking |
| 1.2 | implicit default remote | drop `-r` from every command |
| 2.1 | `vitrina deploy` | zero-SSH deploys from git URL |
| 2.2 | Procfile support | Rails+Sidekiq, web+worker patterns |
| 3.1 | `vitrina bootstrap` | fresh VPS ready in one command |

Phases are independent and can ship in order. Phase 1 items are prerequisites for Phase 2 (deploy needs auto-port and should respect the default remote), but 1.1 and 1.2 can merge separately.
