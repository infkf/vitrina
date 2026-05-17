package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	mcpserver "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func NewServer() *server.MCPServer {
	s := server.NewMCPServer(
		"vitrina",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerTools(s)
	return s
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
}

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

// --- Tool Handlers ---

func handleListApps(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	apps, err := store.List()
	if err != nil {
		return toolError(err.Error())
	}

	health, _ := getBoolArg(req, "health")

	type appInfo struct {
		Subdomain      string `json:"subdomain"`
		FQDN           string `json:"fqdn"`
		Port           int    `json:"port"`
		GitURL         string `json:"git_url,omitempty"`
		GitRef         string `json:"git_ref,omitempty"`
		LastDeployedAt string `json:"last_deployed_at,omitempty"`
		Routing        string `json:"routing"`
		Health         string `json:"health,omitempty"`
	}

	results := make([]appInfo, 0, len(apps))
	for _, app := range apps {
		info := appInfo{
			Subdomain:      app.Subdomain,
			FQDN:           app.FQDN,
			Port:           app.Port,
			GitURL:         app.GitURL,
			GitRef:         app.GitRef,
			LastDeployedAt: app.LastDeployedAt,
			Routing:        routingStatus(cfg, app),
		}
		if health {
			info.Health = healthStatus(app)
		}
		results = append(results, info)
	}

	return toolResult(results)
}

func handleGetApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	type appStatus struct {
		Subdomain      string `json:"subdomain"`
		FQDN           string `json:"fqdn"`
		Port           int    `json:"port"`
		CreatedAt      string `json:"created_at"`
		Scaffold       string `json:"scaffold,omitempty"`
		GitURL         string `json:"git_url,omitempty"`
		GitRef         string `json:"git_ref,omitempty"`
		HealthPath     string `json:"health_path,omitempty"`
		LastDeployedAt string `json:"last_deployed_at,omitempty"`
		EnvKeys        []string `json:"env_keys,omitempty"`
		Routing        string `json:"routing"`
		Containers     string `json:"containers,omitempty"`
		Error          string `json:"error,omitempty"`
	}

	status := appStatus{
		Subdomain:      app.Subdomain,
		FQDN:           app.FQDN,
		Port:           app.Port,
		CreatedAt:      app.CreatedAt,
		Scaffold:       app.Scaffold,
		GitURL:         app.GitURL,
		GitRef:         app.GitRef,
		HealthPath:     app.HealthPath,
		LastDeployedAt: app.LastDeployedAt,
		EnvKeys:        app.EnvKeys,
		Routing:        routingStatus(cfg, app),
	}

	appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
	if info, err := os.Stat(appDir); err == nil && info.IsDir() {
		if deploy.HasDockerCompose(appDir) || deploy.HasDockerfile(appDir) {
			out, err := runCmdInDir(appDir, "docker", "compose", "ps", "--format", "json")
			if err != nil {
				status.Error = err.Error()
			} else {
				status.Containers = out
			}
		} else {
			status.Error = "No Docker containers found for this app"
		}
	} else {
		status.Error = "App directory not found"
	}

	return toolResult(status)
}

func handleAddApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}

	if !isValidSubdomain(subdomain) {
		return toolError(fmt.Sprintf("invalid subdomain %q: must be alphanumeric with optional hyphens, max 63 chars", subdomain))
	}

	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()

	var port int
	if portVal, ok := getNumberArg(req, "port"); ok {
		port = portVal
		if port < 1 || port > 65535 {
			return toolError(fmt.Sprintf("invalid port %d: must be between 1 and 65535", port))
		}
		if port == 80 || port == 443 {
			return toolError(fmt.Sprintf("port %d is reserved for HTTP/HTTPS", port))
		}
	} else {
		port, err = store.NextFreePort(3000)
		if err != nil {
			return toolError(err.Error())
		}
	}

	scaffold, _ := getStringArg(req, "scaffold")
	if scaffold == "" {
		scaffold = "none"
	}

	fqdn := fmt.Sprintf("%s.%s", subdomain, cfg.Domain)

	app := &registry.App{
		Subdomain: subdomain,
		FQDN:      fqdn,
		Port:      port,
		Scaffold:  scaffold,
	}

	if err := caddy.WriteAppConfig(cfg, fqdn, port); err != nil {
		return toolError(fmt.Sprintf("failed to write caddy config: %v", err))
	}

	if err := caddy.Validate(); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		return toolError(fmt.Sprintf("caddy validation failed — changes rolled back: %v", err))
	}

	if err := store.Add(app); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		return toolError(fmt.Sprintf("registration failed — caddy config rolled back: %v", err))
	}

	if err := caddy.Reload(); err != nil {
		return toolText(fmt.Sprintf("App registered but Caddy reload failed: %v\nRun 'vitrina list' to verify.", err))
	}

	return toolText(fmt.Sprintf("Registered: %s -> localhost:%d", fqdn, port))
}

func handleDeployApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}
	repoURL, ok := getStringArg(req, "git_url")
	if !ok || repoURL == "" {
		return toolError("git_url is required")
	}

	if !isValidSubdomain(subdomain) {
		return toolError(fmt.Sprintf("invalid subdomain %q", subdomain))
	}

	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()

	if existing, err := store.Get(subdomain); err == nil && existing != nil {
		return toolError(fmt.Sprintf("subdomain %q already deployed — use redeploy_app to update", subdomain))
	}

	port, err := store.NextFreePort(3000)
	if err != nil {
		return toolError(err.Error())
	}

	branch, _ := getStringArg(req, "branch")
	tag, _ := getStringArg(req, "tag")

	appDir := filepath.Join(cfg.AppsDir, subdomain)

	out, err := runCmd("git", "clone", repoURL, appDir)
	if err != nil {
		return toolError(fmt.Sprintf("clone failed: %v\n%s", err, out))
	}

	gitRef := branch
	if tag != "" {
		gitRef = tag
	}

	if branch != "" {
		out, err := runCmdInDir(appDir, "git", "checkout", branch)
		if err != nil {
			os.RemoveAll(appDir)
			return toolError(fmt.Sprintf("checkout branch failed: %v\n%s", err, out))
		}
	} else if tag != "" {
		out, err := runCmdInDir(appDir, "git", "checkout", "tags/"+tag)
		if err != nil {
			os.RemoveAll(appDir)
			return toolError(fmt.Sprintf("checkout tag failed: %v\n%s", err, out))
		}
	}

	if deploy.HasDockerCompose(appDir) {
		if err := deploy.WriteEnvFile(appDir, port); err != nil {
			os.RemoveAll(appDir)
			return toolError(fmt.Sprintf("failed to write .env: %v", err))
		}
	} else {
		pf, _ := deploy.ParseProcfile(appDir)
		if len(pf) == 0 && !deploy.HasDockerfile(appDir) {
			os.RemoveAll(appDir)
			return toolError("repo has no Dockerfile, Procfile, or docker-compose.yml")
		}
		if err := deploy.WriteCompose(appDir, subdomain, port, pf); err != nil {
			os.RemoveAll(appDir)
			return toolError(fmt.Sprintf("failed to generate docker-compose.yml: %v", err))
		}
	}

	fqdn := fmt.Sprintf("%s.%s", subdomain, cfg.Domain)

	if err := caddy.WriteAppConfig(cfg, fqdn, port); err != nil {
		os.RemoveAll(appDir)
		return toolError(fmt.Sprintf("failed to write caddy config: %v", err))
	}
	if err := caddy.Validate(); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		os.RemoveAll(appDir)
		return toolError(fmt.Sprintf("caddy validation failed — changes rolled back: %v", err))
	}

	app := &registry.App{
		Subdomain:      subdomain,
		FQDN:           fqdn,
		Port:           port,
		Scaffold:       "docker",
		GitURL:         repoURL,
		GitRef:         gitRef,
		LastDeployedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := store.Add(app); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		os.RemoveAll(appDir)
		return toolError(fmt.Sprintf("registration failed — caddy config rolled back: %v", err))
	}

	out, err = runCmdInDir(appDir, "docker", "compose", "up", "-d", "--build")
	if err != nil {
		store.Remove(subdomain)
		caddy.RemoveAppConfig(cfg, fqdn)
		os.RemoveAll(appDir)
		return toolError(fmt.Sprintf("docker compose up failed — changes rolled back: %v\n%s", err, out))
	}

	caddy.Reload()

	return toolText(fmt.Sprintf("Deployed: https://%s", fqdn))
}

func handleRedeployApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}

	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	appDir := filepath.Join(cfg.AppsDir, app.Subdomain)

	branch, _ := getStringArg(req, "branch")
	tag, _ := getStringArg(req, "tag")

	if branch != "" {
		if _, err := runCmdInDir(appDir, "git", "checkout", branch); err != nil {
			return toolError(fmt.Sprintf("checkout branch failed: %v", err))
		}
	} else if tag != "" {
		if _, err := runCmdInDir(appDir, "git", "checkout", "tags/"+tag); err != nil {
			return toolError(fmt.Sprintf("checkout tag failed: %v", err))
		}
	}

	if _, err := runCmdInDir(appDir, "git", "pull"); err != nil {
		return toolError(fmt.Sprintf("git pull failed: %v", err))
	}

	if _, err := runCmdInDir(appDir, "docker", "compose", "up", "-d", "--build"); err != nil {
		return toolError(fmt.Sprintf("docker compose up failed: %v", err))
	}

	app.LastDeployedAt = time.Now().UTC().Format(time.RFC3339)
	if branch != "" {
		app.GitRef = branch
	} else if tag != "" {
		app.GitRef = tag
	}
	if err := store.Update(app); err != nil {
		return toolText(fmt.Sprintf("Redeployed but failed to update metadata: %v", err))
	}

	return toolText(fmt.Sprintf("Redeployed: https://%s", app.FQDN))
}

func handleRemoveApp(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}

	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	if err := caddy.RemoveAppConfig(cfg, app.FQDN); err != nil {
		return toolError(err.Error())
	}

	if err := caddy.Validate(); err != nil {
		caddy.WriteAppConfig(cfg, app.FQDN, app.Port)
		return toolError(fmt.Sprintf("removal caused caddy config error — snippet restored: %v", err))
	}

	if err := store.Remove(subdomain); err != nil {
		caddy.WriteAppConfig(cfg, app.FQDN, app.Port)
		return toolError(fmt.Sprintf("registry removal failed — caddy config restored: %v", err))
	}

	clean, _ := getBoolArg(req, "clean")
	if clean {
		os.RemoveAll(filepath.Join(cfg.AppsDir, subdomain))
	}

	caddy.Reload()

	return toolText(fmt.Sprintf("Removed: %s (was routing to localhost:%d)", app.FQDN, app.Port))
}

func handleLifecycle(action string) func(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	return func(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
		subdomain, ok := getStringArg(req, "subdomain")
		if !ok || subdomain == "" {
			return toolError("subdomain is required")
		}

		if os.Geteuid() != 0 {
			return toolError("this operation requires root privileges (run with sudo)")
		}

		cfg, err := config.Load()
		if err != nil {
			return toolError(err.Error())
		}

		store := registry.New()
		app, err := store.Get(subdomain)
		if err != nil {
			return toolError(err.Error())
		}

		if app.Scaffold != "docker" && app.Scaffold != "" {
			return toolError("lifecycle commands only support docker scaffold apps")
		}

		appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
		out, err := runCmdInDir(appDir, "docker", "compose", action)
		if err != nil {
			return toolError(fmt.Sprintf("failed to %s app: %v\n%s", action, err, out))
		}

		return toolText(fmt.Sprintf("Ran docker compose %s for %s", action, subdomain))
	}
}

func handleEnvList(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
	vars, err := deploy.ReadEnvFile(appDir)
	if err != nil {
		return toolError(err.Error())
	}

	return toolResult(vars)
}

func handleEnvSet(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}
	varsRaw, ok := getMapArg(req, "vars")
	if !ok || len(varsRaw) == 0 {
		return toolError("vars is required and must be a non-empty object")
	}

	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	envVars := make(map[string]string)
	for k, v := range varsRaw {
		if k == "PORT" {
			return toolError("cannot override PORT variable as it is managed by Vitrina")
		}
		envVars[k] = fmt.Sprintf("%v", v)
	}

	appDir := filepath.Join(cfg.AppsDir, subdomain)
	if err := deploy.SetEnvVars(appDir, envVars); err != nil {
		return toolError(err.Error())
	}

	appVars, _ := deploy.ReadEnvFile(appDir)
	app.EnvKeys = []string{}
	for k := range appVars {
		if k != "PORT" {
			app.EnvKeys = append(app.EnvKeys, k)
		}
	}
	if err := store.Update(app); err != nil {
		return toolText(fmt.Sprintf("Environment variables set but failed to update metadata: %v. Run redeploy_app to apply changes.", err))
	}

	return toolText(fmt.Sprintf("Environment variables set for %s. Run redeploy_app to apply changes.", subdomain))
}

func handleEnvUnset(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}
	keysRaw, ok := getSliceArg(req, "keys")
	if !ok || len(keysRaw) == 0 {
		return toolError("keys is required and must be a non-empty array")
	}

	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	keys := make([]string, 0, len(keysRaw))
	for _, k := range keysRaw {
		s, ok := k.(string)
		if !ok {
			continue
		}
		if s == "PORT" {
			return toolError("cannot unset PORT variable as it is managed by Vitrina")
		}
		keys = append(keys, s)
	}

	appDir := filepath.Join(cfg.AppsDir, subdomain)
	if err := deploy.UnsetEnvVars(appDir, keys); err != nil {
		return toolError(err.Error())
	}

	appVars, _ := deploy.ReadEnvFile(appDir)
	app.EnvKeys = []string{}
	for k := range appVars {
		if k != "PORT" {
			app.EnvKeys = append(app.EnvKeys, k)
		}
	}
	if err := store.Update(app); err != nil {
		return toolText(fmt.Sprintf("Environment variables unset but failed to update metadata: %v. Run redeploy_app to apply changes.", err))
	}

	return toolText(fmt.Sprintf("Environment variables unset for %s. Run redeploy_app to apply changes.", subdomain))
}

func handleAppLogs(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	subdomain, ok := getStringArg(req, "subdomain")
	if !ok || subdomain == "" {
		return toolError("subdomain is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return toolError(err.Error())
	}

	appDir := filepath.Join(cfg.AppsDir, app.Subdomain)

	args := []string{"compose", "logs"}
	tail, _ := getStringArg(req, "tail")
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	since, _ := getStringArg(req, "since")
	if since != "" {
		args = append(args, "--since", since)
	}
	servicesRaw, _ := getSliceArg(req, "services")
	for _, s := range servicesRaw {
		if svc, ok := s.(string); ok {
			args = append(args, svc)
		}
	}

	out, err := runCmdInDir(appDir, "docker", args...)
	if err != nil {
		return toolError(fmt.Sprintf("failed to get logs: %v\n%s", err, out))
	}

	return toolText(out)
}

func handlePsApps(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	apps, err := store.List()
	if err != nil {
		return toolError(err.Error())
	}

	type psResult struct {
		Subdomain  string          `json:"subdomain"`
		FQDN       string          `json:"fqdn"`
		Port       int             `json:"port"`
		Containers json.RawMessage `json:"containers,omitempty"`
		Error      string          `json:"error,omitempty"`
	}

	results := make([]psResult, 0, len(apps))
	for _, app := range apps {
		r := psResult{
			Subdomain: app.Subdomain,
			FQDN:      app.FQDN,
			Port:      app.Port,
		}
		appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
		if _, err := os.Stat(appDir); os.IsNotExist(err) {
			r.Error = "no app directory"
		} else {
			out, err := runCmdInDir(appDir, "docker", "compose", "ps", "--format", "json")
			if err != nil {
				r.Error = fmt.Sprintf("docker compose ps failed: %v", err)
			} else {
				r.Containers = json.RawMessage(out)
			}
		}
		results = append(results, r)
	}

	return toolResult(results)
}

func handleDoctor(ctx context.Context, req mcpserver.CallToolRequest) (*mcpserver.CallToolResult, error) {
	if os.Geteuid() != 0 {
		return toolError("this operation requires root privileges (run with sudo)")
	}

	cfg, err := config.Load()
	if err != nil {
		return toolError(err.Error())
	}

	store := registry.New()
	apps, err := store.List()
	if err != nil {
		return toolError(err.Error())
	}

	caddyFiles, err := caddy.ListAppConfigs(cfg)
	if err != nil {
		return toolError(err.Error())
	}

	appDirsEntries, err := os.ReadDir(cfg.AppsDir)
	if err != nil && !os.IsNotExist(err) {
		return toolError(err.Error())
	}

	heal, _ := getBoolArg(req, "heal")

	var issues []string
	var fixes []string

	caddyMap := make(map[string]bool)
	for _, f := range caddyFiles {
		caddyMap[f] = true
	}

	for _, app := range apps {
		expectedCaddy := filepath.Join(cfg.CaddyConfDir, strings.ReplaceAll(app.FQDN, "*", "wildcard")+".caddy")
		if !caddyMap[expectedCaddy] {
			issues = append(issues, fmt.Sprintf("Missing Caddy config for %s", app.Subdomain))
			if heal {
				if err := caddy.WriteAppConfig(cfg, app.FQDN, app.Port); err != nil {
					fixes = append(fixes, fmt.Sprintf("Failed to regenerate Caddy config for %s: %v", app.Subdomain, err))
				} else {
					fixes = append(fixes, fmt.Sprintf("Regenerated Caddy config for %s", app.Subdomain))
				}
			}
		}
		delete(caddyMap, expectedCaddy)
	}

	for danglingFile := range caddyMap {
		issues = append(issues, fmt.Sprintf("Dangling Caddy config: %s", danglingFile))
		if heal {
			if err := os.Remove(danglingFile); err != nil {
				fixes = append(fixes, fmt.Sprintf("Failed to remove %s: %v", danglingFile, err))
			} else {
				fixes = append(fixes, fmt.Sprintf("Removed dangling Caddy config: %s", danglingFile))
			}
		}
	}

	dirMap := make(map[string]bool)
	for _, entry := range appDirsEntries {
		if entry.IsDir() {
			dirMap[entry.Name()] = true
		}
	}

	for _, app := range apps {
		if !dirMap[app.Subdomain] {
			issues = append(issues, fmt.Sprintf("Missing app directory for %s", app.Subdomain))
			if heal {
				if app.GitURL != "" {
					appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
					if out, err := runCmd("git", "clone", app.GitURL, appDir); err != nil {
						fixes = append(fixes, fmt.Sprintf("Failed to re-clone %s: %v\n%s", app.Subdomain, err, out))
					} else {
						if app.GitRef != "" {
							_, _ = runCmdInDir(appDir, "git", "checkout", app.GitRef)
						}
						if err := deploy.WriteEnvFile(appDir, app.Port); err == nil {
							pf, _ := deploy.ParseProcfile(appDir)
							_ = deploy.WriteCompose(appDir, app.Subdomain, app.Port, pf)
							if _, err := runCmdInDir(appDir, "docker", "compose", "up", "-d", "--build"); err != nil {
								fixes = append(fixes, fmt.Sprintf("Re-cloned but failed to start %s", app.Subdomain))
							} else {
								fixes = append(fixes, fmt.Sprintf("Re-cloned and deployed %s", app.Subdomain))
							}
						}
					}
				} else {
					fixes = append(fixes, fmt.Sprintf("Cannot heal missing directory for %s (no Git URL)", app.Subdomain))
				}
			}
		} else {
			if app.Scaffold == "docker" || app.Scaffold == "" {
				appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
				if deploy.HasDockerCompose(appDir) || deploy.HasDockerfile(appDir) {
					if !deploy.ComposeIsRunning(appDir) {
						issues = append(issues, fmt.Sprintf("Containers not running for %s", app.Subdomain))
						if heal {
							if _, err := runCmdInDir(appDir, "docker", "compose", "up", "-d", "--build"); err != nil {
								fixes = append(fixes, fmt.Sprintf("Failed to start containers for %s", app.Subdomain))
							} else {
								fixes = append(fixes, fmt.Sprintf("Started containers for %s", app.Subdomain))
							}
						}
					}
				}
			}
		}
		delete(dirMap, app.Subdomain)
	}

	for danglingDir := range dirMap {
		issues = append(issues, fmt.Sprintf("Dangling app directory: %s (not auto-deleted)", danglingDir))
	}

	type doctorResult struct {
		Issues []string `json:"issues"`
		Fixes  []string `json:"fixes,omitempty"`
		Healed bool     `json:"healed"`
	}

	result := doctorResult{
		Issues: issues,
		Fixes:  fixes,
		Healed: heal,
	}

	if len(issues) == 0 {
		return toolText("All checks passed. Ecosystem is healthy.")
	}

	if heal {
		caddy.Validate()
		caddy.Reload()
	}

	return toolResult(result)
}

// --- Helpers ---

var subdomainPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func isValidSubdomain(s string) bool {
	return subdomainPattern.MatchString(s)
}

func routingStatus(cfg *config.Config, app *registry.App) string {
	if caddy.AppConfigExists(cfg, app.FQDN) {
		return "configured"
	}
	return "missing-snippet"
}

func healthStatus(app *registry.App) string {
	path := app.HealthPath
	if path == "" {
		path = "/"
	}

	client := http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", app.Port, path)
	resp, err := client.Get(url)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return "healthy"
		}
		return fmt.Sprintf("unhealthy (%d)", resp.StatusCode)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", app.Port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "down"
	}
	conn.Close()
	return "reachable (tcp)"
}