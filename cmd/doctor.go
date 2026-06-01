package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system health and optionally heal unsynced state",
	Long: `Scans the registry, Caddy configurations, and Docker containers
for any inconsistencies. If --heal is passed, Vitrina will attempt to
safely fix these issues.`,
	RunE: runRemoteOrLocal(runDoctor),
}

var healFlag bool

func init() {
	doctorCmd.Flags().BoolVar(&healFlag, "heal", false, "Attempt to automatically fix detected issues")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()
	apps, err := store.List()
	if err != nil {
		return err
	}

	caddyFiles, err := caddy.ListAppConfigs(cfg)
	if err != nil {
		return err
	}

	appDirsEntries, err := os.ReadDir(cfg.AppsDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var issues []string
	var fixes []string

	fmt.Println("Running Vitrina checks...")

	// 1. Check for dangling Caddy configs
	caddyMap := make(map[string]bool)
	for _, f := range caddyFiles {
		caddyMap[f] = true
	}

	for _, app := range apps {
		expectedCaddy := filepath.Join(cfg.CaddyConfDir, strings.ReplaceAll(app.FQDN, "*", "wildcard")+".caddy")
		if !caddyMap[expectedCaddy] {
			issues = append(issues, fmt.Sprintf("Missing Caddy config for %s", app.Subdomain))
			if healFlag {
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
		issues = append(issues, fmt.Sprintf("Dangling Caddy config found: %s", danglingFile))
		if healFlag {
			if err := os.Remove(danglingFile); err != nil {
				fixes = append(fixes, fmt.Sprintf("Failed to remove dangling config %s: %v", danglingFile, err))
			} else {
				fixes = append(fixes, fmt.Sprintf("Removed dangling Caddy config: %s", danglingFile))
			}
		}
	}

	// 2. Check for missing/dangling App directories
	dirMap := make(map[string]bool)
	for _, entry := range appDirsEntries {
		if entry.IsDir() {
			dirMap[entry.Name()] = true
		}
	}

	for _, app := range apps {
		appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
		gitDir := filepath.Join(appDir, ".git")
		_, gitErr := os.Stat(gitDir)
		hasGit := gitErr == nil

		isMissing := !dirMap[app.Subdomain] || (app.GitURL != "" && !hasGit)

		if isMissing {
			issues = append(issues, fmt.Sprintf("Missing or incomplete app directory for %s", app.Subdomain))
			if healFlag {
				if app.GitURL != "" {
					fmt.Printf("Attempting to heal %s by cloning %s...\n", app.Subdomain, app.GitURL)
					
					// Backup .env if it exists
					envPath := filepath.Join(appDir, ".env")
					envBakPath := filepath.Join(cfg.AppsDir, app.Subdomain+".env.bak")
					hasEnv := false
					if _, err := os.Stat(envPath); err == nil {
						if err := os.Rename(envPath, envBakPath); err == nil {
							hasEnv = true
						}
					}

					// Remove the incomplete directory to allow clean git clone
					_ = os.RemoveAll(appDir)

					if err := deploy.CloneRepo(app.GitURL, appDir, false); err != nil {
						fixes = append(fixes, fmt.Sprintf("Failed to re-clone %s: %v", app.Subdomain, err))
						if hasEnv {
							_ = os.MkdirAll(appDir, 0755)
							_ = os.Rename(envBakPath, envPath)
						}
					} else {
						if app.GitRef != "" {
							if err := deploy.CheckoutBranch(appDir, app.GitRef); err != nil {
								_ = deploy.CheckoutTag(appDir, app.GitRef)
							}
						}
						
						// Restore the backed-up .env file
						if hasEnv {
							_ = os.Rename(envBakPath, envPath)
						}

						if err := deploy.WriteEnvFile(appDir, app.Port); err == nil {
							// Check if we need to write Procfile to Compose
							pf, _ := deploy.ParseProcfile(appDir)
							_ = deploy.WriteCompose(appDir, app.Subdomain, app.Port, pf)
							if err := deploy.ComposeUp(appDir, false); err == nil {
								fixes = append(fixes, fmt.Sprintf("Re-cloned and deployed missing app directory for %s", app.Subdomain))
							} else {
								fixes = append(fixes, fmt.Sprintf("Re-cloned but failed to start %s: %v", app.Subdomain, err))
							}
						}
					}
				} else {
					fixes = append(fixes, fmt.Sprintf("Cannot heal missing directory for %s (no Git URL recorded). Please 'remove' and 'add' it again.", app.Subdomain))
				}
			}
		} else {
			appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
			if deploy.HasDockerCompose(appDir) || deploy.HasDockerfile(appDir) {
				if !deploy.ComposeIsRunning(appDir) {
					issues = append(issues, fmt.Sprintf("Docker containers for %s are not running", app.Subdomain))
					if healFlag {
						if err := deploy.ComposeUp(appDir, false); err != nil {
							fixes = append(fixes, fmt.Sprintf("Failed to start containers for %s: %v", app.Subdomain, err))
						} else {
							fixes = append(fixes, fmt.Sprintf("Started containers for %s", app.Subdomain))
						}
					}
				}
			}
		}
		delete(dirMap, app.Subdomain)
	}

	for danglingDir := range dirMap {
		issues = append(issues, fmt.Sprintf("Dangling app directory found: %s", danglingDir))
		if healFlag {
			fixes = append(fixes, fmt.Sprintf("Skipped deleting dangling directory %s (safety precaution)", danglingDir))
		}
	}

	// 3. Check for non-vitrina Docker containers using ports that overlap with
	//    vitrina's managed ports (3000+), and detect orphaned containers whose
	//    name matches the vitrina convention (<subdomain>-<service>-N) but whose
	//    subdomain is no longer in the registry.
	vitrinaPorts := make(map[int]string)
	for _, app := range apps {
		vitrinaPorts[app.Port] = app.Subdomain
	}
	externalPorts, _ := scanDockerPorts()
	// Build a set of known vitrina container name prefixes so we don't flag
	// containers we manage ourselves (e.g. "lemon-app-1" belongs to "lemon").
	vitrinaPrefixes := make(map[string]bool)
	for _, app := range apps {
		vitrinaPrefixes[app.Subdomain+"-"] = true
	}
	vitrinaContainerRe := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)-.+-\d+$`)
	// Track subdomains we've already flagged as orphans to avoid duplicates.
	orphanSubdomains := make(map[string]bool)
	for hostPort, containerName := range externalPorts {
		if hostPort < 3000 {
			continue
		}
		// Check if this container belongs to a vitrina-managed app
		isManaged := false
		for prefix := range vitrinaPrefixes {
			if strings.HasPrefix(containerName, prefix) {
				isManaged = true
				break
			}
		}
		if isManaged {
			continue
		}
		if sub, known := vitrinaPorts[hostPort]; known {
			issues = append(issues, fmt.Sprintf("Port %d is used by both vitrina app %q and external container %q", hostPort, sub, containerName))
			continue
		}
		// Check if this looks like an orphaned vitrina container
		if m := vitrinaContainerRe.FindStringSubmatch(containerName); m != nil {
			possibleSub := m[1]
			if !orphanSubdomains[possibleSub] {
				issues = append(issues, fmt.Sprintf("Container %q looks like an orphan from a removed vitrina app (possible subdomain: %q)", containerName, possibleSub))
				orphanSubdomains[possibleSub] = true
			}
		} else {
			issues = append(issues, fmt.Sprintf("External container %q exposes port %d (not managed by vitrina)", containerName, hostPort))
		}
	}

	if len(issues) == 0 {
		fmt.Println("✔ All checks passed. Ecosystem is healthy.")
		return nil
	}

	fmt.Printf("\nFound %d issue(s):\n", len(issues))
	for _, issue := range issues {
		fmt.Printf("  - %s\n", issue)
	}

	if !healFlag {
		fmt.Println("\nRun 'vitrina doctor --heal' to attempt automatic fixes.")
		return nil
	}

	fmt.Println("\nHealing Results:")
	for _, fix := range fixes {
		fmt.Printf("  - %s\n", fix)
	}

	fmt.Println("Reloading Caddy to apply config changes...")
	_ = caddy.Validate()
	if err := caddy.Reload(); err != nil {
		fmt.Printf("Warning: failed to reload Caddy: %v\n", err)
	}

	return nil
}

// scanDockerPorts runs "docker ps" and extracts published host port -> container
// name mappings from all running containers.
func scanDockerPorts() (map[int]string, error) {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Ports}}").Output()
	if err != nil {
		return nil, err
	}
	ports := make(map[int]string)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		portPart := parts[1]
		for _, mapping := range strings.Split(portPart, ",") {
			mapping = strings.TrimSpace(mapping)
			// Match patterns like "127.0.0.1:3001->3000/tcp" or "0.0.0.0:80->80/tcp"
			before, _, ok := strings.Cut(mapping, "->")
			if !ok {
				continue
			}
			hostPortStr := before[strings.LastIndex(before, ":")+1:]
			hostPort, err := strconv.Atoi(hostPortStr)
			if err != nil {
				continue
			}
			ports[hostPort] = name
		}
	}
	return ports, nil
}
