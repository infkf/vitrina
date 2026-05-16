package cmd

import (
	"fmt"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
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

var (
	updateDomain string
	updateEmail  string
)

func init() {
	configUpdateCmd.Flags().StringVarP(&updateDomain, "domain", "d", "", "New root domain")
	configUpdateCmd.Flags().StringVarP(&updateEmail, "email", "e", "", "New ACME email")
	configCmd.AddCommand(configUpdateCmd)
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
			if err := store.Update(app); err != nil {
				fmt.Printf("Warning: failed to update app %s in registry: %v\n", app.Subdomain, err)
			}
			
			_ = caddy.RemoveAppConfig(cfg, oldFQDN)
			if err := caddy.WriteAppConfig(cfg, app.FQDN, app.Port); err != nil {
				fmt.Printf("Warning: failed to write caddy config for %s: %v\n", app.Subdomain, err)
			}
		}
		fmt.Printf("Updated FQDNs for %d apps.\n", len(apps))
	}

	if err := caddy.Validate(); err != nil {
		fmt.Printf("Warning: Caddy validation failed after updates: %v\n", err)
	} else {
		_ = caddy.Reload()
		fmt.Println("Caddy reloaded successfully.")
	}

	return nil
}
