package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/infkf/vitrina/internal/caddy"
	"github.com/infkf/vitrina/internal/clierror"
	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/deploy"
	"github.com/infkf/vitrina/internal/output"
	"github.com/infkf/vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove <subdomain>",
	Short: "Remove an app from the proxy",
	Long: `Deletes the app's Caddy routing snippet, removes it from the
registry, and reloads Caddy gracefully.

Boilerplate files in /etc/vitrina/apps/<subdomain>/ are left in place
so you can inspect or reuse them before deleting manually.`,
	Args: cobra.ExactArgs(1),
	RunE: runRemoteOrLocal(runRemove),
}

var removeClean bool

func init() {
	removeCmd.Flags().BoolVarP(&removeClean, "clean", "c", false,
		"Also remove boilerplate files under /etc/vitrina/apps/<subdomain>/")
	rootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	subdomain := args[0]
	if removeClean && !confirmDestructive && !dryRun {
		return &clierror.ClassifiedError{Code: clierror.CodeInteraction, Category: "safety", Err: fmt.Errorf("--clean is destructive; re-run with --yes or --dry-run")}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()

	app, err := store.Get(subdomain)
	if err != nil {
		return err
	}
	if dryRun {
		plan := map[string]any{
			"dry_run": true,
			"planned_changes": []map[string]any{
				{"action": "remove", "target": filepath.Join(cfg.CaddyConfDir, app.FQDN+".caddy")},
			},
			"risk":                  "destructive",
			"requires_confirmation": removeClean,
		}
		if removeClean {
			plan["planned_changes"] = append(plan["planned_changes"].([]map[string]any), map[string]any{"action": "destroy", "target": "docker.volumes"}, map[string]any{"action": "delete", "target": filepath.Join(cfg.AppsDir, subdomain)})
		}
		data, _ := json.Marshal(plan)
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	if err := caddy.RemoveAppConfig(cfg, app.FQDN); err != nil {
		return err
	}

	if err := caddy.Validate(cfg); err != nil {
		if writeErr := caddy.WriteAppConfig(cfg, app.FQDN, app.Port); writeErr != nil {
			return fmt.Errorf("removal caused config error AND rollback failed:\n  config error: %w\n  rollback error: %v", err, writeErr)
		}
		return fmt.Errorf("removal caused a caddy config error — snippet restored (no changes made):\n%w", err)
	}

	if err := store.Remove(subdomain); err != nil {
		caddy.WriteAppConfig(cfg, app.FQDN, app.Port)
		return fmt.Errorf("registry removal failed — caddy config restored: %w", err)
	}

	if err := caddy.Reload(cfg); err != nil {
		output.Warnf("app removed from config but Caddy reload failed: %v\nRun 'caddy reload --config /etc/caddy/Caddyfile' manually.", err)
	} else {
		output.Successf("Removed: %s (was routing to localhost:%d)", app.FQDN, app.Port)
	}

	if removeClean {
		tearDownContainers(cmd, cfg.AppsDir, subdomain)
		cleanBoilerplate(cmd, cfg.AppsDir, subdomain)
	}

	return nil
}

func cleanBoilerplate(cmd *cobra.Command, appsDir, subdomain string) {
	dir := filepath.Join(appsDir, subdomain)
	if err := os.RemoveAll(dir); err != nil {
		output.Warnf("failed to remove boilerplate dir %s: %v", dir, err)
	} else {
		fmt.Printf("Cleaned boilerplate: %s\n", dir)
	}
}

func tearDownContainers(cmd *cobra.Command, appsDir, subdomain string) {
	dir := filepath.Join(appsDir, subdomain)
	if !deploy.HasDockerCompose(dir) {
		return
	}
	fmt.Printf("Tearing down Docker containers for %s...\n", subdomain)
	if err := deploy.ComposeCommand(dir, "down", "-v"); err != nil {
		output.Warnf("failed to tear down containers for %s: %v", subdomain, err)
	}
}
