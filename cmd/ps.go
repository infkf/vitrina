package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/deploy"
	"github.com/infkf/vitrina/internal/registry"

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
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Println("No apps registered.")
		}
		return nil
	}

	type psOut struct {
		App        *registry.App   `json:"app"`
		Containers json.RawMessage `json:"containers,omitempty"`
		Error      string          `json:"error,omitempty"`
	}

	var results []psOut

	for _, app := range apps {
		appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
		
		var raw json.RawMessage
		var errMsg string

		if _, err := os.Stat(appDir); os.IsNotExist(err) {
			errMsg = "no app directory — registered via 'add', not 'deploy'"
		} else {
			out, err := deploy.ComposePS(appDir, jsonOutput)
			if err != nil {
				errMsg = fmt.Sprintf("docker compose ps failed: %v", err)
			} else if jsonOutput && len(out) > 0 {
				raw = out
			}
		}

		if jsonOutput {
			results = append(results, psOut{App: app, Containers: raw, Error: errMsg})
		} else {
			fmt.Fprintf(os.Stdout, "=== %s (%s → localhost:%d) ===\n", app.Subdomain, app.FQDN, app.Port)
			if errMsg != "" {
				fmt.Printf("  %s\n", errMsg)
			}
			fmt.Println()
		}
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	return nil
}
