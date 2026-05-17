# Vitrina — Thoughts & Direction

## Origin

This whole thing started because I was too lazy to deploy an app somewhere. Spinning up a VPS, installing Docker, writing Caddy configs by hand, remembering which port runs what — it's all tedious makework for a single server. Vitrina is the tool I wished I had: one command to go from `git clone` to live HTTPS.

The original ROADMAP.md phases are all done. Most of the "obvious gaps" from the first version of this file are now shipped too. So this is round two — what's left after the low-hanging fruit.

## What Shipped Since v1

These were all listed as missing in the first draft. Now they're real:

- **`vitrina stop/start/restart`** — Lifecycle commands for pausing and bouncing containers without rebuilding. Clean and small.
- **`vitrina status <subdomain>`** — Consolidated view: routes, git info, env keys, container state, deployment metadata. Exactly the "one command" that was missing.
- **`vitrina config update`** — Change domain/email after init. Regenerates Caddyfile and updates all app FQDNs in the registry. Handles the most common post-init need.
- **App metadata tracking** — `GitURL`, `GitRef`, `LastDeployedAt`, `EnvKeys` (and reserved `HealthPath`) now stored in the registry. `redeploy` updates `LastDeployedAt` and `GitRef`. `env set`/`unset` maintain `EnvKeys`. This was the foundation for `doctor` and makes `status` meaningful.
- **`vitrina doctor [--heal]`** — Cross-references registry, Caddy configs, app directories, and Docker containers. Missing Caddy snippets get regenerated. Dangling ones get deleted. Missing app directories get re-cloned from `GitURL`. Stopped containers get restarted. Dangling directories are intentionally NOT deleted (safety precaution).
- **`vitrina export` / `vitrina import`** — Backup and restore. Archives registry, Caddy snippets, and `.env` files. Works locally and remotely via `-r`. Import runs `doctor --heal` afterward to restore consistency.
- **`.vitrinaignore`** — `push` now reads a `.vitrinaignore` file and passes it to tar via `--exclude-from`. Still excludes `.git` by default.
- **`--json` output** — Global flag on `list`, `ps`, `env list`, and `status`. Makes the CLI scriptable.
- **Better `logs`** — `--tail N`, `--since`, and per-service filtering within a multi-service app. No longer a bare passthrough.
- **HTTP health checks** — `list --health` now does an HTTP GET to `HealthPath` (default `/`) before falling back to TCP dial. Catches apps that are listening but returning errors.

## What's Still Missing

### Gaps that matter now

**Tests for new code.** There are no tests for `doctor`, `status`, `export`, `import`, `config update`, or the lifecycle commands. The internal packages have tests for the new helper functions (`ListAppConfigs`, `ComposeIsRunning`, `ComposeCommand`, `ComposePS` with JSON, `ReadEnvFile`, `SetEnvVars`, `UnsetEnvVars`), but the command layer is untested. Integration tests that exercise `add` → `list` → `remove` or `deploy` → `doctor --heal` would catch cross-package regressions.

**SSH output capture and error context.** `RunCommand` and `RunScript` still stream directly to stdout/stderr. There's no programmatic way to capture output, parse it, or make decisions based on it. This matters most for bootstrap and deploy — if `vitrina init` fails on a remote, the error message is just an exit code. Enriching remote errors with the command, host, and truncated stderr would make debugging dramatically easier.

**Alpine support in bootstrap.** The install script handles apt, yum, and dnf but not `apk`. Low effort, removes a class of "why doesn't this work" issues.

**`config update` and domain changes.** Changing the domain updates all FQDNs in the registry and regenerates Caddy snippets, but it doesn't handle DNS or certificate implications. Caddy will auto-provision new certificates, but there's no warning about the old ones or the DNS records that need updating. At minimum, the command should print a reminder.

### Bigger swings for later

**Webhook-triggered auto-deploy.** A lightweight listener that maps incoming push events to registered apps via `GitURL`. Natural next step now that git metadata is tracked. Would need a long-running process, which is a new pattern for Vitrina.

**Secret management for `.env`.** Files are 0644 plaintext. `env list` prints values in clear. Could mask values by default, encrypt at rest, or integrate with `sops`. The `HealthPath` field shows the schema can accommodate new features without breaking changes.

**Smarter `doctor`.** Today it re-clones and rebuilds missing directories, but it could do more: detect image drift (container running stale image), detect `.env` mismatches between registry and file, detect port conflicts not caught by the registry alone. The framework is there.

## Architecture Observations

**The `Update` method changed everything.** Before `registry.Update()`, changing any app metadata required `remove` + `add`. Now `redeploy` and `env set`/`unset` can mutate in place. This is cleaner but also means the registry is no longer append-only — any command can change any field. Worth being deliberate about which commands touch which fields.

**`doctor --heal` is conservative by design.** It won't delete dangling app directories (safety precaution). It won't remove stopped containers, only restart them. It won't touch things it can't verify. This is the right instinct — a healing tool that causes damage is worse than no healing tool.

**`import` calls `doctor --heal` by invoking `os.Args[0]`.** This works but is fragile — if the binary path has spaces, or if running in a test harness, it could break. Should call `runDoctor` directly instead of shelling out to the same binary.

**`config update` with domain changes touches N apps.** When you change the domain, it iterates all apps, updates FQDNs, removes old Caddy snippets, writes new ones. If any step fails partway through, you're left in an inconsistent state. This should have rollback semantics like `add` and `deploy`.

## The Bigger Question

The same question remains from v1, but the answer is clearer now: stay single-server, keep polishing. The features that shipped since v1 (doctor, status, lifecycle, metadata tracking, export/import) all share one trait — they make the existing single-VPS workflow more reliable without adding conceptual weight. That's the right direction.

**MCP server is live.** `vitrina mcp` exposes all Vitrina operations as Model Context Protocol tools over stdio. AI agents (Claude, Cursor, opencode) can call `deploy_app`, `list_apps`, `doctor` with `--heal`, etc. directly without shell parsing. The `VITRINA_REMOTE` env var lets the MCP server run on your laptop while delegating all operations to the VPS via SSH — using the same `-r` flag infrastructure the CLI already has. This makes the "too lazy to deploy" origin story recursive: let the AI do the deploying, from wherever you are.

The MCP server doesn't change the architecture — it's a thin adapter over the same internal packages. But it does make the "too lazy to deploy" origin story recursive: let the AI do the deploying.

Multi-host, dashboards, user management — those are different products. The simplicity is still the feature.

## Principles to Keep

- **One command, one job.** `deploy` clones and wires up. `redeploy` pulls and rebuilds. `remove` cleans everything. `doctor` diagnoses, `doctor --heal` fixes. Keep compositing simple.
- **Fail safe, fail loud.** The rollback pattern is in `add`, `remove`, `deploy`. `config update` needs it too. Every mutation that touches the filesystem or external services should have rollback semantics.
- **Conservative healing.** `doctor --heal` doesn't delete things it can't verify. This instinct is right. Don't let automation become a footgun.
- **Stdlib first.** Only Cobra, pflag, and mcp-go as dependencies. Resist the urge to pull in logging frameworks, YAML parsers, or HTTP routers. The simplicity is a feature.
- **Root required for mutation, not for reading.** `list` doesn't need root. `ps` arguably doesn't either. Keep the door open for non-root observability.
- **Metadata is for machines too.** `--json` output, `EnvKeys` snapshots, `LastDeployedAt` timestamps — these aren't just for humans reading terminals. They're for scripts, agents, and future tooling. Keep adding structured output.