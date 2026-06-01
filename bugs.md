# Vitrina — Bug Report & Design Gaps

## Root Cause: Why LLMs Bypass MCP and Run Giant SSH Commands

The core problem is a **missing `push` MCP tool**. The CLI has `vitrina push <subdomain> [local_dir]` which packages a local directory and deploys directly to a remote VPS — exactly what an LLM working on local code needs. But the MCP server never exposes it. The LLM is forced to fall back to shell commands.

Even when the LLM does use git-based deployment (which is the intended flow), the friction is high:

- `env_set` tells the LLM "run redeploy_app to apply changes" but never does it automatically. The CLI has `--apply` for this. Two MCP calls for one logical operation.
- `redeploy_app` assumes changes already reached the remote git repo. The LLM still needs to `git add/commit/push` first, and there's no MCP tool for that.
- `remove_app` doesn't tear down containers. Re-deploy on the same subdomain fails with a port collision because Docker containers from the old deployment are still running. The LLM's mental model breaks.

After 2-3 such failures, the LLM learns that manual `vitrina` shell commands are more reliable than the MCP tools, and bypasses MCP entirely.

---

## Critical Bugs

### 1. `DownloadFile` hangs on first SSH connection
**File:** `internal/remote/remote.go:267-286`

SCP args for `DownloadFile` are built manually without `-o StrictHostKeyChecking=accept-new`, `-o PasswordAuthentication=no`, or `-o ConnectTimeout=10`. Every other SSH/SCP path in the codebase includes these. First-time connections hang forever waiting for host-key confirmation. Breaks `export`/`import` over remote.

```diff
 func DownloadFile(r *Remote, remotePath, localPath string) error {
-    args := []string{}
+    args := []string{
+        "-o", "StrictHostKeyChecking=accept-new",
+        "-o", "PasswordAuthentication=no",
+        "-o", "ConnectTimeout=10",
+    }
```

### 2. MCP `remove_app` doesn't tear down Docker containers
**File:** `internal/mcp/local.go:399-442`

The CLI `remove --clean` runs `docker compose down -v` before `os.RemoveAll`. The MCP handler just deletes the directory. Orphaned containers survive in Docker, consuming the port. Re-deploying on the same subdomain fails with a port collision.

```go
// Missing before line 439:
if clean {
    _, _ = runCmdInDir(appDir, "docker", "compose", "down", "-v")  // NEVER CALLED
    os.RemoveAll(filepath.Join(cfg.AppsDir, subdomain))
}
```

### 3. MCP `add_app` never calls `scaffold.Generate`
**File:** `internal/mcp/local.go:133-203`

The `scaffold` parameter is parsed (line 170) and stored (line 181), but `scaffold.Generate()` is never called. If a user passes `--scaffold docker` via MCP, no files are written to the app directory. The CLI `add.go` calls `scaffold.Generate()` at line 82-86.

### 4. MCP doctor missing Docker port conflict scan
**File:** `internal/mcp/local.go:699-866`

The CLI `doctor` command scans `docker ps` output for port conflicts with non-Vitrina containers (lines 270-305 of `cmd/doctor.go`). The MCP handler completely skips this check. An external process consuming a port that Vitrina wants to use will be silently missed.

### 5. `env_set`/`env_unset` return without applying changes
**Files:** `internal/mcp/local.go:504-609`, `cmd/env.go`

Both MCP tools write `.env` and update registry metadata, then return with "Run redeploy_app to apply changes." The CLI has `--apply` which calls `ComposeRestart` immediately. The LLM must chain two MCP calls for one logical operation. Chaining failure rate is high — LLMs often forget step 2.

### 6. `redeploy_app` never calls `caddy.Reload()`
**Files:** `cmd/redeploy.go:106`, `internal/mcp/local.go:396`

Every other mutating command (`deploy`, `add`, `remove`) reloads Caddy after changes. `redeploy` does not. If a redeploy regenerates compose with new service names or structural changes, Caddy routes may become stale.

---

## Code Duplication (Root of Inconsistency)

### 7. `internal/mcp/local.go` (928 lines) duplicates all `cmd/*.go` logic
**Files:** `internal/mcp/local.go` vs `cmd/deploy.go`, `cmd/redeploy.go`, `cmd/add.go`, `cmd/remove.go`, `cmd/env.go`, `cmd/doctor.go`, `cmd/list.go`, `cmd/status.go`, `cmd/ps.go`

This is the **primary source of behavioral drift** that confuses LLMs. The MCP local handlers reimplement every CLI command instead of calling the shared internal packages. Specific drift already present:

| Feature | CLI behavior | MCP behavior |
|---|---|---|
| `deploy` quiet mode | `--quiet` suppresses build output + prints timing | No quiet mode at all |
| `deploy` rollback on fail | `docker compose down -v` → registry remove → caddy remove → rm dir | Just `os.RemoveAll(appDir)` |
| `add` scaffold | Calls `scaffold.Generate()` | Stores `Scaffold` field in struct, never generates files |
| `remove` clean | `docker compose down -v` + remove dir | Just `os.RemoveAll` |
| `doctor` Docker scan | Scans `docker ps` for port conflicts | No scan |
| `env set/unset` apply | `--apply` flag calls `ComposeRestart` | No apply; user must call `redeploy_app` separately |
| Subdomain regex | Defined in `cmd/add.go:22` | Defined again in `mcp/server.go:572` |
| `routingStatus`/`healthStatus` | Defined in `cmd/list.go:88-125` | Duplicated in `mcp/local.go:868-899` |

Fix: Either export the `run*` functions from `cmd/` and call them from the MCP handlers, or extract shared logic into `internal/` packages that both sides consume.

---

## Missing MCP Tools

### 8. No `push` MCP tool

The CLI `vitrina push <subdomain> [local_dir]` packages a local directory and deploys to a remote VPS. This is the **single most important missing tool** — it's exactly what LLMs need when they make local code changes and want to deploy. Without it, LLMs try to:

- Run `vitrina push ...` via bash with giant tar+upload scripts
- SSH into the VPS and manually write files
- Guess at `git add/commit/push` workflows when they don't have push access

### 9. No `reload` MCP tool

No way to reload Caddy via MCP. If an LLM manually edits Caddy configs (which they shouldn't need to, but do when working around other gaps), they can't reload.

### 10. No `export`/`import` MCP tools

Backup and restore workflows require shell commands.

---

## Logic Gaps

### 11. `runRemoteOrLocal` enforces local root for remote ops
**File:** `cmd/root.go:52-66`

```go
func runRemoteOrLocal(fn func(...) error) func(...) error {
    return func(cmd *cobra.Command, args []string) error {
        name := remoteName
        if name == "" {
            if cfg, err := remote.Load(); err == nil && cfg.Default != "" {
                name = cfg.Default      // err from Load() is ignored after err == nil
            }
        }
        if name != "" {
            return remote.Execute(name, cmd, args)  // fn is skipped, so requireRoot() never runs
        }
        return fn(cmd, args)  // runs requireRoot() locally
    }
}
```

The callback `fn` is skipped when a remote is active, so `requireRoot()` is never called for remote ops. This is actually correct behavior — the remote user handles root. But the code is confusing: `fn` is declared but selectively called. If the remote path is active, the local user doesn't need root, which is correct. The issue is minor architectural confusion, not a bug.

Correction from earlier analysis: This is actually working as intended. The `fn` (which contains `requireRoot()`) is skipped for remote execution, which is correct since the remote user handles privileges.

### 12. Git error auto-recovery is locale-dependent
**File:** `internal/deploy/deploy.go:69-72`

```go
if strings.Contains(errStr, "diverged") || strings.Contains(errStr, "divergent") ||
    strings.Contains(errStr, "untracked") || strings.Contains(errStr, "overwritten") ||
    strings.Contains(errStr, "local changes") || strings.Contains(errStr, "merge") {
    return forcePull(dir, ref, quiet)
}
```

These are English-specific error strings. A Git configured with `LANG=fr_FR.UTF-8` or `LANG=ja_JP.UTF-8` will emit different messages and the fallback to `forcePull` silently fails.

### 13. Systemd scaffold hardcodes `/opt/<subdomain>` path
**File:** `internal/scaffold/scaffold.go:43-63`

```go
WorkingDirectory=/opt/%s  // Wrong: apps live at /etc/vitrina/apps/<subdomain>
User=www-data              // Doesn't exist on CentOS/RHEL (uses nginx/apache)
```

The scaffold doesn't match the actual deployment directory, and the user assumption is Debian-specific.

### 14. `WriteEnvFile` trailing newline inconsistency
**File:** `internal/deploy/deploy.go:199-210`

When updating an existing `.env` file, `strings.Join(lines, "\n")` preserves whatever trailing newline the original file had. If the original had none, the new file also has none. If the original had `\n`, the result has a trailing newline. Minor but can cause diffs/linters to complain.

### 15. `subdomainRegex` duplicated in 3 files
**Files:** `cmd/add.go:22`, `cmd/doctor.go:18`, `internal/mcp/server.go:572`

Three independent copies of the same regex. If the subdomain validation rule changes (e.g., RFC 1035 compliance tightened), all three must be updated. One copy has already been missed on changes before.

### 16. `filterArgs` false-positive on `-remove`-like flags
**File:** `internal/remote/remote.go:217`

```go
if strings.HasPrefix(arg, "-r") && arg != "-r" {
    continue  // catches -rprod correctly, but also catches --remove, --retry
}
```

No current commands use flags starting with `-r` except `--remote` itself, but it's a latent bug.

### 17. Shell injection risk in export/import/push remote commands
**Files:** `cmd/export.go:63-64`, `cmd/import.go:66-67`, `cmd/push.go:158-162`

```go
fmt.Sprintf(`%s export %s`, r.VitrinaPath, remoteOut)  // no shellEscape
```

`r.VitrinaPath` and file paths are inserted into shell commands without escaping. `shellEscape()` exists in the remote package but isn't used here. If paths contain spaces or special characters, the command breaks.

### 18. Non-deterministic default remote selection on removal
**File:** `cmd/remote.go:219-224`

When removing the current default remote, a new default is picked from `range cfg.Remotes` — Go map iteration order is randomized. Users get an unpredictable new default.

---

## Low Priority

### 19. `mcp-go` listed as indirect dependency
**File:** `go.mod:11`

`github.com/mark3labs/mcp-go` is used directly in `cmd/mcp.go` and `internal/mcp/server.go` but listed as `// indirect`. Should be a direct dependency.

### 20. `requireRoot()` called for commands that may work without root
**Files:** `cmd/ps.go:28`, `cmd/logs.go:34`

Docker commands can work without root if the user is in the `docker` group. The blanket `requireRoot()` check is unnecessarily restrictive.

### 21. `deploy --quiet` output is discarded on failure with no debug info
**File:** `internal/deploy/deploy.go:375-393`

When quiet mode fails, the captured output IS printed (line 388: `fmt.Print(buf.String())`). This is actually correct. The earlier analysis flagged this but it works as intended.

### 22. `caddy.Validate()` and `caddy.Reload()` hardcode `/etc/caddy/Caddyfile`
**File:** `internal/caddy/caddy.go:66-69,79`

These functions use hardcoded paths while `WriteAppConfig` and `ListAppConfigs` derive paths from `*config.Config`. If `Config.CaddyConfDir` ever changes, these functions silently target the wrong file.
