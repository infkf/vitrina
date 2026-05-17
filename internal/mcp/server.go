package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	mcpserver "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var remoteName string

func NewServer() *server.MCPServer {
	remoteName = os.Getenv("VITRINA_REMOTE")

	s := server.NewMCPServer(
		"vitrina",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerTools(s)
	return s
}

// isRemote returns true when VITRINA_REMOTE is set, meaning all operations
// should be delegated to the remote VPS via SSH.
func isRemote() bool {
	return remoteName != ""
}

// remoteExec runs a vitrina CLI command on the remote VPS and returns its output.
// It uses the same -r flag that the CLI uses, so SSH config, keys, and sudo
// handling all work identically to manual CLI usage.
func remoteExec(args ...string) (string, error) {
	fullArgs := append([]string{"-r", remoteName, "--json"}, args...)
	cmd := exec.Command("vitrina", fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("vitrina %s failed: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// remoteExecNoJSON runs a vitrina CLI command on the remote without --json.
// Used for commands that don't support JSON output.
func remoteExecNoJSON(args ...string) (string, error) {
	fullArgs := append([]string{"-r", remoteName}, args...)
	cmd := exec.Command("vitrina", fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("vitrina %s failed: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func registerTools(s *server.MCPServer) {
	s.AddTool(mcpserver.NewTool("list_apps",
		mcpserver.WithDescription("List all registered apps with their proxy status and optional health checks"),
		mcpserver.WithBoolean("health",
			mcpserver.Description("Check whether each app's port is accepting connections (HTTP then TCP)"),
		),
	), handleListApps)

	s.AddTool(mcpserver.NewTool("get_app",
		mcpserver.WithDescription("Get detailed status of an app including git info, environment, and container state"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("The subdomain of the app"),
		),
	), handleGetApp)

	s.AddTool(mcpserver.NewTool("add_app",
		mcpserver.WithDescription("Register a new app and wire it into the Caddy reverse proxy"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain for the app (e.g. 'blog' for blog.example.com)"),
		),
		mcpserver.WithNumber("port",
			mcpserver.Description("Port to assign (auto-assigned from 3000 if omitted)"),
		),
		mcpserver.WithString("scaffold",
			mcpserver.Description("Generate boilerplate: docker, systemd, or none (default: none)"),
			mcpserver.Enum("docker", "systemd", "none"),
		),
	), handleAddApp)

	s.AddTool(mcpserver.NewTool("deploy_app",
		mcpserver.WithDescription("Clone a git repository, generate Docker Compose config, and deploy the app"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain for the app"),
		),
		mcpserver.WithString("git_url",
			mcpserver.Required(),
			mcpserver.Description("Git repository URL to clone"),
		),
		mcpserver.WithString("branch",
			mcpserver.Description("Git branch to checkout after cloning"),
		),
		mcpserver.WithString("tag",
			mcpserver.Description("Git tag to checkout after cloning (overrides branch)"),
		),
	), handleDeployApp)

	s.AddTool(mcpserver.NewTool("redeploy_app",
		mcpserver.WithDescription("Pull latest changes and rebuild the app's Docker containers"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app to redeploy"),
		),
		mcpserver.WithString("branch",
			mcpserver.Description("Git branch to checkout"),
		),
		mcpserver.WithString("tag",
			mcpserver.Description("Git tag to checkout (overrides branch)"),
		),
		mcpserver.WithBoolean("force",
			mcpserver.Description("Use force mode to handle force-pushed branches"),
		),
	), handleRedeployApp)

	s.AddTool(mcpserver.NewTool("remove_app",
		mcpserver.WithDescription("Remove an app from the proxy and optionally clean up its files"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app to remove"),
		),
		mcpserver.WithBoolean("clean",
			mcpserver.Description("Also remove the app directory under /etc/vitrina/apps/"),
		),
	), handleRemoveApp)

	s.AddTool(mcpserver.NewTool("stop_app",
		mcpserver.WithDescription("Stop an app's Docker containers without removing them"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
	), handleLifecycle("stop"))

	s.AddTool(mcpserver.NewTool("start_app",
		mcpserver.WithDescription("Start an app's Docker containers"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
	), handleLifecycle("start"))

	s.AddTool(mcpserver.NewTool("restart_app",
		mcpserver.WithDescription("Restart an app's Docker containers"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
	), handleLifecycle("restart"))

	s.AddTool(mcpserver.NewTool("env_list",
		mcpserver.WithDescription("List environment variables for an app"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
	), handleEnvList)

	s.AddTool(mcpserver.NewTool("env_set",
		mcpserver.WithDescription("Set environment variables for an app. Run redeploy after to apply changes."),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
		mcpserver.WithObject("vars",
			mcpserver.Required(),
			mcpserver.Description("Key-value pairs of environment variables to set (e.g. {\"DATABASE_URL\": \"postgres://...\"})"),
		),
	), handleEnvSet)

	s.AddTool(mcpserver.NewTool("env_unset",
		mcpserver.WithDescription("Unset environment variables for an app. Run redeploy after to apply changes."),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
		mcpserver.WithArray("keys",
			mcpserver.Required(),
			mcpserver.WithStringItems(mcpserver.Description("Environment variable names to unset")),
		),
	), handleEnvUnset)

	s.AddTool(mcpserver.NewTool("app_logs",
		mcpserver.WithDescription("Get container logs for an app"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
		mcpserver.WithString("tail",
			mcpserver.Description("Number of recent lines to show (e.g. '100')"),
		),
		mcpserver.WithString("since",
			mcpserver.Description("Show logs since timestamp (e.g. '2024-01-01T00:00:00') or relative (e.g. '42m')"),
		),
		mcpserver.WithArray("services",
			mcpserver.WithStringItems(mcpserver.Description("Specific service names within the app's docker-compose")),
		),
	), handleAppLogs)

	s.AddTool(mcpserver.NewTool("ps_apps",
		mcpserver.WithDescription("List running Docker containers for all registered apps"),
	), handlePsApps)

	s.AddTool(mcpserver.NewTool("doctor",
		mcpserver.WithDescription("Check system health by cross-referencing registry, Caddy configs, app directories, and Docker containers"),
		mcpserver.WithBoolean("heal",
			mcpserver.Description("Attempt to automatically fix detected issues (re-gen missing Caddy configs, re-clone missing app dirs, restart stopped containers)"),
		),
	), handleDoctor)

	s.AddTool(mcpserver.NewTool("set_health_path",
		mcpserver.WithDescription("Set or clear the health check path for an app (the URL path queried by vitrina list --health)"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app"),
		),
		mcpserver.WithString("path",
			mcpserver.Description("HTTP path for health checks (e.g. '/health'). Omit or set to empty to clear and reset to default '/'."),
		),
	), handleSetHealthPath)
}

// --- Result helpers ---

func toolError(msg string) (*mcpserver.CallToolResult, error) {
	return &mcpserver.CallToolResult{
		Content: []mcpserver.Content{mcpserver.NewTextContent(msg)},
		IsError: true,
	}, nil
}

func toolResult(data any) (*mcpserver.CallToolResult, error) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("Failed to marshal result: %v", err))
	}
	return &mcpserver.CallToolResult{
		Content: []mcpserver.Content{mcpserver.NewTextContent(string(jsonBytes))},
	}, nil
}

func toolText(msg string) (*mcpserver.CallToolResult, error) {
	return &mcpserver.CallToolResult{
		Content: []mcpserver.Content{mcpserver.NewTextContent(msg)},
	}, nil
}

// --- Arg helpers ---

func getStringArg(req mcpserver.CallToolRequest, name string) (string, bool) {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok || val == nil {
		return "", false
	}
	s, ok := val.(string)
	return s, ok
}

func getBoolArg(req mcpserver.CallToolRequest, name string) (bool, bool) {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok || val == nil {
		return false, false
	}
	switch v := val.(type) {
	case bool:
		return v, true
	case string:
		return v == "true", true
	}
	return false, false
}

func getNumberArg(req mcpserver.CallToolRequest, name string) (int, bool) {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok || val == nil {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func getMapArg(req mcpserver.CallToolRequest, name string) (map[string]any, bool) {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok || val == nil {
		return nil, false
	}
	m, ok := val.(map[string]any)
	return m, ok
}

func getSliceArg(req mcpserver.CallToolRequest, name string) ([]any, bool) {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok || val == nil {
		return nil, false
	}
	s, ok := val.([]any)
	return s, ok
}

// --- Tool Handlers ---
// When VITRINA_REMOTE is set, all handlers delegate to the CLI over SSH.
// Otherwise, they call internal packages directly (requires root on the local machine).

func handleListApps(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	if isRemote() {
		args := []string{"list"}
		if health, ok := getBoolArg(req, "health"); ok && health {
			args = append(args, "--health")
		}
		out, err := remoteExec(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleListAppsLocal(ctx, req)
}

func handleGetApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		out, err := remoteExec("status", subdomain)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleGetAppLocal(ctx, req)
}

func handleAddApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		args := []string{"add", subdomain}
		if port, ok := getNumberArg(req, "port"); ok {
			args = append(args, fmt.Sprintf("%d", port))
		}
		if scaffold, ok := getStringArg(req, "scaffold"); ok && scaffold != "" {
			args = append(args, "-s", scaffold)
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleAddAppLocal(ctx, req)
}

func handleDeployApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	gitURL, _ := getStringArg(req, "git_url")
	if isRemote() {
		args := []string{"deploy", subdomain, gitURL}
		if branch, ok := getStringArg(req, "branch"); ok && branch != "" {
			args = append(args, "--branch", branch)
		}
		if tag, ok := getStringArg(req, "tag"); ok && tag != "" {
			args = append(args, "--tag", tag)
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleDeployAppLocal(ctx, req)
}

func handleRedeployApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		args := []string{"redeploy", subdomain}
		if branch, ok := getStringArg(req, "branch"); ok && branch != "" {
			args = append(args, "--branch", branch)
		}
		if tag, ok := getStringArg(req, "tag"); ok && tag != "" {
			args = append(args, "--tag", tag)
		}
		if force, ok := getBoolArg(req, "force"); ok && force {
			args = append(args, "--force")
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleRedeployAppLocal(ctx, req)
}

func handleRemoveApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		args := []string{"remove", subdomain}
		if clean, ok := getBoolArg(req, "clean"); ok && clean {
			args = append(args, "-c")
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleRemoveAppLocal(ctx, req)
}

func handleLifecycle(action string) func(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	return func(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
		subdomain, _ := getStringArg(req, "subdomain")
		if isRemote() {
			out, err := remoteExecNoJSON(action, subdomain)
			if err != nil {
				return toolError(err.Error())
			}
			return toolText(out)
		}
		return handleLifecycleLocal(ctx, req, action)
	}
}

func handleEnvList(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		out, err := remoteExec("env", "list", subdomain)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleEnvListLocal(ctx, req)
}

func handleEnvSet(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	varsRaw, _ := getMapArg(req, "vars")
	if isRemote() {
		args := []string{"env", "set", subdomain}
		for k, v := range varsRaw {
			args = append(args, fmt.Sprintf("%s=%v", k, v))
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleEnvSetLocal(ctx, req)
}

func handleEnvUnset(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	keysRaw, _ := getSliceArg(req, "keys")
	if isRemote() {
		args := []string{"env", "unset", subdomain}
		for _, k := range keysRaw {
			if s, ok := k.(string); ok {
				args = append(args, s)
			}
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleEnvUnsetLocal(ctx, req)
}

func handleAppLogs(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		args := []string{"logs", subdomain, "--follow=false"}
		if tail, ok := getStringArg(req, "tail"); ok && tail != "" {
			args = append(args, "--tail", tail)
		}
		if since, ok := getStringArg(req, "since"); ok && since != "" {
			args = append(args, "--since", since)
		}
		if services, ok := getSliceArg(req, "services"); ok {
			for _, s := range services {
				if svc, ok := s.(string); ok {
					args = append(args, svc)
				}
			}
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleAppLogsLocal(ctx, req)
}

func handlePsApps(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	if isRemote() {
		out, err := remoteExec("ps")
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handlePsAppsLocal(ctx, req)
}

func handleDoctor(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	if isRemote() {
		args := []string{"doctor"}
		if heal, ok := getBoolArg(req, "heal"); ok && heal {
			args = append(args, "--heal")
		}
		out, err := remoteExecNoJSON(args...)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleDoctorLocal(ctx, req)
}

func handleSetHealthPath(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	if isRemote() {
		path, _ := getStringArg(req, "path")
		if path == "" {
			out, err := remoteExecNoJSON("config", "health-path", subdomain, "--clear")
			if err != nil {
				return toolError(err.Error())
			}
			return toolText(out)
		}
		out, err := remoteExecNoJSON("config", "health-path", subdomain, path)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(out)
	}
	return handleSetHealthPathLocal(ctx, req)
}

// --- Local implementations (require root + local /etc/vitrina) ---

var subdomainPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func isValidSubdomain(s string) bool {
	return subdomainPattern.MatchString(s)
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s failed: %w\n%s", name, err, string(out))
	}
	return string(out), nil
}

func runCmdInDir(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s failed: %w\n%s", name, err, string(out))
	}
	return string(out), nil
}