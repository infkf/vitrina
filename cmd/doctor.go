package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
		if !dirMap[app.Subdomain] {
			issues = append(issues, fmt.Sprintf("Missing app directory for %s", app.Subdomain))
			if healFlag {
				if app.GitURL != "" {
					appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
					fmt.Printf("Attempting to heal %s by cloning %s...\n", app.Subdomain, app.GitURL)
					if err := deploy.CloneRepo(app.GitURL, appDir); err != nil {
						fixes = append(fixes, fmt.Sprintf("Failed to re-clone %s: %v", app.Subdomain, err))
					} else {
						if app.GitRef != "" {
							if err := deploy.CheckoutBranch(appDir, app.GitRef); err != nil {
								_ = deploy.CheckoutTag(appDir, app.GitRef)
							}
						}
						if err := deploy.WriteEnvFile(appDir, app.Port); err == nil {
							// Check if we need to write Procfile to Compose
							pf, _ := deploy.ParseProcfile(appDir)
							_ = deploy.WriteCompose(appDir, app.Subdomain, app.Port, pf)
							if err := deploy.ComposeUp(appDir); err == nil {
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
			// Directory exists. Check Docker containers if scaffold is docker
			if app.Scaffold == "docker" || app.Scaffold == "" {
				appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
				if deploy.HasDockerCompose(appDir) || deploy.HasDockerfile(appDir) {
					if !deploy.ComposeIsRunning(appDir) {
						issues = append(issues, fmt.Sprintf("Docker containers for %s are not running", app.Subdomain))
						if healFlag {
							if err := deploy.ComposeUp(appDir); err != nil {
								fixes = append(fixes, fmt.Sprintf("Failed to start containers for %s: %v", app.Subdomain, err))
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
		issues = append(issues, fmt.Sprintf("Dangling app directory found: %s", danglingDir))
		if healFlag {
			fixes = append(fixes, fmt.Sprintf("Skipped deleting dangling directory %s (safety precaution)", danglingDir))
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
