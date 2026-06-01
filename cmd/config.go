package cmd

import (
	"fmt"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/output"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Vitrina configuration",
}

var configUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update global configuration (domain, email)",
	RunE:  runRemoteOrLocal(runConfigUpdate),
}

var configHealthPathCmd = &cobra.Command{
	Use:   "health-path <subdomain> [path]",
	Short: "Set or clear the health check path for an app",
	Long: `Set the HTTP path used for health checks (e.g., /health). Use --clear to remove.

The health check path determines which URL is queried when running
"vitrina list --health". Default is "/".`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runRemoteOrLocal(runConfigHealthPath),
}

var (
	updateDomain         string
	updateEmail          string
	configHealthPathClear bool
)

func init() {
	configUpdateCmd.Flags().StringVarP(&updateDomain, "domain", "d", "", "New root domain")
	configUpdateCmd.Flags().StringVarP(&updateEmail, "email", "e", "", "New ACME email")
	configHealthPathCmd.Flags().BoolVarP(&configHealthPathClear, "clear", "c", false, "Clear the health path (reset to default)")
	configCmd.AddCommand(configUpdateCmd)
	configCmd.AddCommand(configHealthPathCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigUpdate(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	changed := false
	if updateDomain != "" && updateDomain != cfg.Domain {
		cfg.Domain = updateDomain
		changed = true
	}
	if updateEmail != "" && updateEmail != cfg.Email {
		cfg.Email = updateEmail
		changed = true
	}

	if !changed {
		fmt.Println("No changes to apply.")
		return nil
	}

	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Println("Configuration updated.")

	if updateEmail != "" {
		if err := caddy.WriteMainCaddyfile(cfg.Email); err != nil {
			return fmt.Errorf("failed to update Main Caddyfile: %w", err)
		}
		fmt.Println("Regenerated Main Caddyfile.")
	}

	if updateDomain != "" {
		store := registry.New()
		apps, _ := store.List()
		for _, app := range apps {
			oldFQDN := app.FQDN
			app.FQDN = fmt.Sprintf("%s.%s", app.Subdomain, cfg.Domain)

			if err := caddy.WriteAppConfig(cfg, app.FQDN, app.Port); err != nil {
				output.Warnf("failed to write caddy config for %s — old config preserved: %v", app.Subdomain, err)
				continue
			}

			if err := caddy.RemoveAppConfig(cfg, oldFQDN); err != nil {
				output.Warnf("failed to remove old caddy config for %s: %v", app.Subdomain, err)
			}

			if err := store.Update(app); err != nil {
				output.Warnf("failed to update app %s in registry: %v", app.Subdomain, err)
			}
		}
		fmt.Printf("Updated FQDNs for %d apps.\n", len(apps))
	}

	if err := caddy.Validate(); err != nil {
		output.Warnf("Caddy validation failed after updates: %v", err)
	} else {
		_ = caddy.Reload()
		fmt.Println("Caddy reloaded successfully.")
	}

	return nil
}

func runConfigHealthPath(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	subdomain := args[0]
	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return err
	}

	if configHealthPathClear {
		app.HealthPath = ""
	} else {
		if len(args) < 2 {
			return fmt.Errorf("path is required (use --clear to clear the health path)")
		}
		app.HealthPath = args[1]
	}

	if err := store.Update(app); err != nil {
		return err
	}

	if configHealthPathClear {
		fmt.Printf("Cleared health path for %s\n", subdomain)
	} else {
		fmt.Printf("Set health path for %s to %s\n", subdomain, app.HealthPath)
	}

	return nil
}
