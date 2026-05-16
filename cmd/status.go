package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <subdomain>",
	Short: "Show consolidated status of an app",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteOrLocal(runStatus),
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	subdomain := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil {
		return err
	}

	type statusOut struct {
		App        *registry.App   `json:"app"`
		Containers json.RawMessage `json:"containers,omitempty"`
		Error      string          `json:"error,omitempty"`
	}

	appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
	var raw json.RawMessage
	var errMsg string

	if deploy.HasDockerCompose(appDir) || deploy.HasDockerfile(appDir) {
		out, err := deploy.ComposePS(appDir, jsonOutput)
		if err != nil {
			errMsg = fmt.Sprintf("docker compose ps failed: %v", err)
		} else if jsonOutput && len(out) > 0 {
			raw = out
		}
	} else {
		errMsg = "No Docker containers found for this app."
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(statusOut{App: app, Containers: raw, Error: errMsg})
	}

	fmt.Printf("App:        %s\n", app.Subdomain)
	fmt.Printf("URL:        https://%s\n", app.FQDN)
	fmt.Printf("Port:       %d\n", app.Port)
	fmt.Printf("Created:    %s\n", app.CreatedAt)
	fmt.Println()

	fmt.Println("=== Source ===")
	if app.GitURL != "" {
		fmt.Printf("Git URL:    %s\n", app.GitURL)
		if app.GitRef != "" {
			fmt.Printf("Git Ref:    %s\n", app.GitRef)
		} else {
			fmt.Printf("Git Ref:    (default branch)\n")
		}
		if app.LastDeployedAt != "" {
			fmt.Printf("Deployed:   %s\n", app.LastDeployedAt)
		}
	} else {
		fmt.Println("Not deployed from Git (local push or manual add)")
	}
	fmt.Println()

	fmt.Println("=== Environment ===")
	if len(app.EnvKeys) > 0 {
		fmt.Printf("Keys:       %s\n", strings.Join(app.EnvKeys, ", "))
	} else {
		fmt.Println("Keys:       (none)")
	}
	fmt.Println()

	fmt.Println("=== Containers ===")
	if errMsg != "" {
		fmt.Println(errMsg)
	} else {
		_, _ = deploy.ComposePS(appDir, false) // print to stdout
	}

	return nil
}
