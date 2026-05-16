# Vitrina — Thoughts & Direction

## Origin

This whole thing started because I was too lazy to deploy an app somewhere. Spinning up a VPS, installing Docker, writing Caddy configs by hand, remembering which port runs what — it's all tedious makework for a single server. Vitrina is the tool I wished I had: one command to go from `git clone` to live HTTPS.

The ROADMAP.md phases are all done. Everything on the original list shipped. So this is the inflection point — where does it go next?

## What's Working Well

- **One app = one Caddy snippet** is the right abstraction. It means a bad config for one app never takes down another, and cleanup is just deleting a file.
- **Auto-port assignment** removes an entire class of "which port do I use" decisions.
- **Rollback on failure** in `add`/`remove`/`deploy` means the system stays consistent even when things go wrong.
- **Bootstrap** getting a fresh VPS from zero to ready in one command is genuinely useful.
- **Remote execution** (`-r`) means you can manage everything from your laptop without SSHing in manually.

All of this works. The question isn't whether it works — it's what's missing that you'd actually reach for.

## What's Missing

### The obvious gaps

**`vitrina stop` and `vitrina restart`.** The lifecycle right now is deploy or remove. There's no way to pause an app or restart it without rebuilding. These are trivial to implement and the absence is felt every time you just want to bounce a container.

**`vitrina status <subdomain>`.** Right now getting the full picture of an app means running `list`, `env list`, and `ps` separately. One command that shows routes, env, container state, and git info would be the natural companion to `list`.

**`vitrina config update`.** `init` refuses to run if config already exists, which is the right safety check. But there's no way to change your domain or email after setup without editing JSON by hand. A simple update command fixes this.

**Store git metadata in the registry.** The `App` struct has no memory of where it came from. `redeploy` assumes the repo is already cloned and knows the remote, but the registry can't tell you. Adding `GitURL`, `Branch`, and `LastDeployedAt` fields would make `status`, `redeploy`, and future CI/CD features smarter.

### The next tier

**Better `logs`.** Currently a bare passthrough to `docker compose logs`. Adding `--tail N`, `--since`, and per-service filtering within a multi-service app would make it actually useful for debugging.

**HTTP health checks.** The `--health` flag on `list` does a TCP dial. That catches apps that aren't listening, but not apps that are listening and returning 500s. An HTTP GET to a configurable path (default `/`) would catch more real failures.

**`vitrina export` / `vitrina import`.** Backup and restore of the registry, Caddy snippets, and `.env` files. Essential for disaster recovery or migrating to a new VPS. Without this, recreating a server means running `add` for every app from memory.

**`.vitrinaignore` for `push`.** The tar archive only excludes `.git`. A `.vitrinaignore` file would let you skip `node_modules`, build artifacts, local `.env` files, and whatever else shouldn't end up on the server.

**`--json` output mode.** A global flag on `list`, `ps`, `env list`, and `status` that outputs structured JSON instead of tabwriter. This is what makes a CLI tool scriptable and composable. Without it, any automation has to parse human-readable tables.

### Bigger swings

**SSH output capture.** `RunCommand` and `RunScript` stream directly to stdout/stderr with no programmatic way to capture or inspect the output. This makes error handling in bootstrap and deploy fragile — you can't programmatically detect what went wrong. Returning structured errors with host, command, and exit code would make remote operations debuggable.

**Webhook-triggered auto-deploy.** A lightweight listener that triggers `redeploy` on git push events. This is the natural next step once you have git metadata in the registry — `vitrina deploy --watch` or a small webhook endpoint that maps incoming push events to registered apps.

**Secret management for `.env`.** The `.env` files are 0644 plaintext on disk. `env list` prints values to the terminal in clear. At minimum, values could be encrypted at rest and masked on output. Long term, integration with something like `sops` or age encryption would be ideal, but even a thin wrapper is better than plaintext.

**Alpine support in bootstrap.** The install script handles apt, yum, and dnf but not apk. Small addition, removes a class of "why doesn't this work" issues.

## The Bigger Question

Vitrina is a single-server tool. That's the whole point — it's for one VPS, one person, a handful of apps. It shouldn't try to become Kubernetes. But there are directions worth considering:

- **Does it stay single-server forever?** If yes, the focus should be on polish — better observability, safer operations, smoother DX. The "previous tier" items above.
- **Does it go multi-host?** If yes, that's a fundamentally different product. You'd need app placement, inter-host networking, fleet-wide health checks, and a coordination layer. That's a hard pivot and probably a different tool.
- **Does it become a platform?** Adding webhooks, a dashboard, and user management turns it into a mini-Heroku. Tempting, but that's a full project with a different scope.

My bias: stay single-server, polish what's there, and add the obvious gaps. The value of this tool is its simplicity. Every feature should make the single-VPS workflow better without adding cognitive overhead for the "I just want to deploy this thing" use case.

## Principles to Keep

- **One command, one job.** `deploy` clones and wires up. `redeploy` pulls and rebuilds. `remove` cleans everything. Keep compositing simple.
- **Fail safe, fail loud.** The rollback pattern is good. Keep it. Expand it.
- **Stdlib first.** Only Cobra as a dependency. Resist the urge to pull in logging frameworks, YAML parsers, or HTTP routers. The simplicity is a feature.
- **Root required for mutation, not for reading.** `list` doesn't need root. `ps` arguably doesn't either. Keep the door open for non-root observability.