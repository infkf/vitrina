package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "Show container status for all deployed apps",
	Args:  cobra.NoArgs,
	RunE:  runRemoteOrLocal(runPS),
}

func init() {
	rootCmd.AddCommand(psCmd)
}

func runPS(_ *cobra.Command, _ []string) error {
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

	if len(apps) == 0 {
		fmt.Println("No apps registered.")
		return nil
	}

	for _, app := range apps {
		appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
		fmt.Fprintf(os.Stdout, "=== %s (%s → localhost:%d) ===\n", app.Subdomain, app.FQDN, app.Port)
		if _, err := os.Stat(appDir); os.IsNotExist(err) {
			fmt.Println("  (no app directory — registered via 'add', not 'deploy')")
		} else if err := deploy.ComposePS(appDir); err != nil {
			fmt.Printf("  docker compose ps failed: %v\n", err)
		}
		fmt.Println()
	}

	return nil
}
