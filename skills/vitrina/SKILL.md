---
name: vitrina
description: Use ONLY when deploying, redeploying, or managing apps on a Vitrina-managed VPS. Triggers on keywords: vitrina, deploy, redeploy, push, subdomain, Caddy, docker compose, env var, doctor, VPS, bootstrap. Also triggers when a .vitrina.json file exists in the project root — this marker means the app is deployed via Vitrina. Do NOT use for general Docker questions unrelated to vitrina.
---

# Vitrina Deployment Skill

Vitrina is a PaaS-lite CLI for managing web apps on a Linux VPS. **Run `vitrina` commands directly via bash. There are no MCP tools — the CLI is the sole interface.**

## Detection: How to Know This Is a Vitrina-Managed App

If the project root contains a `.vitrina.json` file, the app is deployed and managed by Vitrina. This file is written automatically during `deploy` and `push`. Example contents:

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

When you see this file, use `vitrina` CLI commands for all deployment and infrastructure operations.

## Remote vs Local

Run `vitrina remote list` to see configured remotes. If a default remote is set, or if you pass `-r <name>`, commands execute on the remote VPS via SSH. Otherwise they run locally (requires root).

```bash
vitrina -r prod list           # run on remote "prod"
vitrina list                   # run locally (needs root / /etc/vitrina)
```

## Available CLI Commands

| Command | Args | Flags | Notes |
|---------|------|-------|-------|
| `vitrina list` | — | `--health` | List all registered apps; `--health` TCP-checks ports |
| `vitrina status` | `<subdomain>` | `--json` | Detailed app info: git, env, containers |
| `vitrina add` | `<subdomain> [port]` | `-s/--scaffold` (docker\|systemd\|none) | Register a manually-run app; auto-assigns port from 3000 |
| `vitrina deploy` | `<subdomain> <git-url>` | `--branch`, `--tag`, `-q/--quiet` | Clone → compose → Caddy → up; `-q` suppresses build output |
| `vitrina redeploy` | `<subdomain>` | `--branch`, `--tag`, `-q/--quiet`, `--force` | Pull → docker compose up --build → caddy reload; `--force` uses fetch+reset |
| `vitrina push` | `<subdomain> [local_dir]` | — | Package local dir and deploy directly — no git required |
| `vitrina remove` | `<subdomain>` | `-c/--clean` | Remove from proxy; `-c` also runs docker compose down -v and deletes dir |
| `vitrina env list` | `<subdomain>` | — | List env vars |
| `vitrina env set` | `<subdomain>` | `--apply` | Set env vars; `--apply` restarts containers immediately |
| `vitrina env unset` | `<subdomain>` | `--apply` | Unset env vars; `--apply` restarts containers immediately |
| `vitrina stop/start/restart` | `<subdomain>` | — | Container lifecycle |
| `vitrina logs` | `<subdomain>` | `-f`, `--tail`, `--since` | docker compose logs passthrough |
| `vitrina ps` | — | `--json` | docker compose ps for all apps |
| `vitrina doctor` | — | `--heal` | Diagnose ecosystem; `--heal` auto-fixes |
| `vitrina reload` | — | — | Reload Caddy (retry TLS certs) |
| `vitrina bootstrap` | `<remote>` | `--domain`, `--email`, `--binary` | Fresh VPS setup: Docker + Caddy + binary + init |

## Workflows

### 1. Deploy a new app from git

```bash
vitrina -r prod deploy myapp https://github.com/user/repo.git --branch main -q
```

Clones, detects Dockerfile/Procfile/docker-compose.yml, generates config, registers in Caddy, builds and starts containers. Port is auto-assigned from 3000. `-q` suppresses build output — prints timing summary instead.

### 2. Deploy local code changes (NO git required)

```bash
vitrina -r prod push myapp .
```

Packages the current directory, uploads, extracts, and redeploys. Use this when you've made local file changes and need to deploy immediately. Do NOT manually SSH, scp, or write files on the VPS.

### 3. Pull and rebuild an already-deployed app

```bash
vitrina -r prod redeploy myapp -q
```

Pulls latest from the git remote. With `--force`: uses fetch+reset for diverged histories. Builds and reloads Caddy.

### 4. Set environment variables

```bash
vitrina -r prod env set myapp --apply
# then edit the file that opens, or pipe: echo '{"KEY":"value"}' | vitrina -r prod env set myapp --apply
```

Writes to `.env` and `--apply` restarts containers immediately. Do NOT follow with `redeploy` — the restart happens inline.

### 5. Remove an app

```bash
vitrina -r prod remove myapp          # remove from proxy + registry
vitrina -r prod remove myapp -c       # also tear down containers + delete dir
```

### 6. Diagnose problems

```bash
vitrina -r prod doctor                # report only
vitrina -r prod doctor --heal         # auto-fix
```

## Rules

- **Do** run `vitrina` commands via bash — this is the intended interface.
- **Do NOT** manually SSH into the VPS and edit/sftp files. Use `vitrina push`.
- **Do NOT** call `env set --apply` then `redeploy`. `--apply` already restarts containers.
- **Do NOT** manually write Caddy configs. Use `vitrina add`/`deploy`/`remove`.
- **Do NOT** chain `git add/commit/push` + `vitrina redeploy` when `vitrina push` works without git.
- **Do NOT** run `git add/commit` unless the user explicitly asks you to commit. `vitrina push` deploys without touching git history.

## Post-Deployment Verification

```bash
vitrina -r prod list --health   # all apps routing and healthy?
vitrina -r prod status myapp    # containers running? correct branch?
vitrina -r prod ps              # containers actually up?
vitrina -r prod logs myapp --tail 20  # any errors?
```

## Checking If Deployed Code Is Current

When the user says "I pushed to git, is it live?" or "are we running the latest code?", use `vitrina status` with `--json` to check the deployed git ref. Then:

- If the deployed git ref is behind origin, run `vitrina -r prod redeploy <subdomain> -q`.
- If you have local changes that haven't been committed, use `vitrina -r prod push <subdomain> .` instead.
- After redeploying or pushing, verify with `vitrina -r prod logs <subdomain> --tail 20`.

## Port & Routing

- Vitrina auto-assigns ports from 3000 upward, skipping 80/443.
- Each app gets `subdomain.<domain>` with automatic TLS via Caddy.
- The `PORT` env var is injected automatically — apps should read `$PORT`.
- Caddy routes to `127.0.0.1:<port>`. Only the `web` process gets a public route (in Procfile setups).

## Environment Variables

- `PORT` is reserved and managed by Vitrina. Do NOT override it.
- `env set` uses `--apply` to restart containers after writing `.env`.
- `env unset KEY1 KEY2 --apply` removes keys and restarts.
