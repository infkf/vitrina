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

func isRemote() bool {
	return remoteName != ""
}

func runVitrina(args ...string) (*mcpserver.CallToolResult, error) {
	if isRemote() {
		args = append([]string{"-r", remoteName}, args...)
	}
	cmd := exec.Command("vitrina", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return toolError(fmt.Sprintf("vitrina %s: %v\n%s", strings.Join(args, " "), err, string(out)))
	}
	return toolText(string(out))
}

func runVitrinaLocal(args ...string) (*mcpserver.CallToolResult, error) {
	cmd := exec.Command("vitrina", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return toolError(fmt.Sprintf("vitrina %s: %v\n%s", strings.Join(args, " "), err, string(out)))
	}
	return toolText(string(out))
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

	s.AddTool(mcpserver.NewTool("push_app",
		mcpserver.WithDescription("Package a local directory and deploy it to the server without requiring git push"),
		mcpserver.WithString("subdomain",
			mcpserver.Required(),
			mcpserver.Description("Subdomain of the app to push to"),
		),
		mcpserver.WithString("dir",
			mcpserver.Description("Local directory to push (defaults to current directory)"),
		),
	), handlePushApp)

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
		mcpserver.WithDescription("Set environment variables for an app. Containers are restarted immediately to apply changes."),
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
		mcpserver.WithDescription("Unset environment variables for an app. Containers are restarted immediately to apply changes."),
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

	s.AddTool(mcpserver.NewTool("logs_all",
		mcpserver.WithDescription("Get recent container logs from all registered apps at once"),
		mcpserver.WithString("tail",
			mcpserver.Description("Number of recent lines to show from each app (e.g. '100')"),
		),
	), handleLogsAll)

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

	s.AddTool(mcpserver.NewTool("reload",
		mcpserver.WithDescription("Reload Caddy to apply config changes and retry TLS certificates"),
	), handleReload)

	s.AddTool(mcpserver.NewTool("monitor",
		mcpserver.WithDescription("Run a one-off health check across all apps, checking TLS cert expiry, HTTPS status, and container liveness"),
	), handleMonitor)
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

func handleListApps(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	args := []string{"list", "--json"}
	if health, ok := getBoolArg(req, "health"); ok && health {
		args = append(args, "--health")
	}
	return runVitrina(args...)
}

func handleGetApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	return runVitrina("status", subdomain, "--json")
}

func handleAddApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	args := []string{"add", subdomain}
	if port, ok := getNumberArg(req, "port"); ok {
		args = append(args, fmt.Sprintf("%d", port))
	}
	if scaffold, ok := getStringArg(req, "scaffold"); ok && scaffold != "" {
		args = append(args, "-s", scaffold)
	}
	return runVitrina(args...)
}

func handleDeployApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	gitURL, _ := getStringArg(req, "git_url")
	args := []string{"deploy", subdomain, gitURL, "-q"}
	if branch, ok := getStringArg(req, "branch"); ok && branch != "" {
		args = append(args, "--branch", branch)
	}
	if tag, ok := getStringArg(req, "tag"); ok && tag != "" {
		args = append(args, "--tag", tag)
	}
	return runVitrina(args...)
}

func handleRedeployApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	args := []string{"redeploy", subdomain, "-q"}
	if branch, ok := getStringArg(req, "branch"); ok && branch != "" {
		args = append(args, "--branch", branch)
	}
	if tag, ok := getStringArg(req, "tag"); ok && tag != "" {
		args = append(args, "--tag", tag)
	}
	if force, ok := getBoolArg(req, "force"); ok && force {
		args = append(args, "--force")
	}
	return runVitrina(args...)
}

func handleRemoveApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	args := []string{"remove", subdomain}
	if clean, ok := getBoolArg(req, "clean"); ok && clean {
		args = append(args, "-c")
	}
	return runVitrina(args...)
}

func handlePushApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	args := []string{"push", subdomain}
	if dir, ok := getStringArg(req, "dir"); ok && dir != "" {
		args = append(args, dir)
	}
	return runVitrinaLocal(args...)
}

func handleLifecycle(action string) func(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	return func(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
		subdomain, _ := getStringArg(req, "subdomain")
		return runVitrina(action, subdomain)
	}
}

func handleEnvList(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	return runVitrina("env", "list", subdomain, "--json")
}

func handleEnvSet(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	varsRaw, _ := getMapArg(req, "vars")
	args := []string{"env", "set", subdomain}
	for k, v := range varsRaw {
		args = append(args, fmt.Sprintf("%s=%v", k, v))
	}
	args = append(args, "--apply")
	return runVitrina(args...)
}

func handleEnvUnset(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	keysRaw, _ := getSliceArg(req, "keys")
	args := []string{"env", "unset", subdomain}
	for _, k := range keysRaw {
		if s, ok := k.(string); ok {
			args = append(args, s)
		}
	}
	args = append(args, "--apply")
	return runVitrina(args...)
}

func handleAppLogs(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
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
	return runVitrina(args...)
}

func handlePsApps(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	return runVitrina("ps", "--json")
}

func handleDoctor(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	args := []string{"doctor"}
	if heal, ok := getBoolArg(req, "heal"); ok && heal {
		args = append(args, "--heal")
	}
	return runVitrina(args...)
}

func handleSetHealthPath(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, _ := getStringArg(req, "subdomain")
	path, _ := getStringArg(req, "path")
	if path == "" {
		return runVitrina("config", "health-path", subdomain, "--clear")
	}
	return runVitrina("config", "health-path", subdomain, path)
}

func handleReload(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	return runVitrina("reload")
}

func handleLogsAll(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	args := []string{"logs", "--all", "--follow=false"}
	if tail, ok := getStringArg(req, "tail"); ok && tail != "" {
		args = append(args, "--tail", tail)
	}
	return runVitrina(args...)
}

func handleMonitor(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	return runVitrina("monitor", "--once")
}

// --- Validation ---

var subdomainPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func isValidSubdomain(s string) bool {
	return subdomainPattern.MatchString(s)
}
