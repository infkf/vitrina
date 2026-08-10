package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/deploy"
	"github.com/infkf/vitrina/internal/registry"

	"github.com/spf13/cobra"
)

func buildLifecycleCmd(action string) *cobra.Command {
	short := fmt.Sprintf("%s an app's containers without rebuilding", strings.ToUpper(string(action[0]))+action[1:])
	return &cobra.Command{
		Use:   fmt.Sprintf("%s <subdomain>", action),
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: runRemoteOrLocal(func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}

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

			appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
			if !deploy.HasDockerCompose(appDir) && !deploy.HasDockerfile(appDir) {
				return fmt.Errorf("lifecycle commands only support docker scaffold apps")
			}

			fmt.Printf("Running docker compose %s for %s...\n", action, subdomain)
			if err := deploy.ComposeCommand(appDir, action); err != nil {
				return fmt.Errorf("failed to %s app: %w", action, err)
			}
			return nil
		}),
	}
}

func init() {
	rootCmd.AddCommand(buildLifecycleCmd("stop"))
	rootCmd.AddCommand(buildLifecycleCmd("start"))
	rootCmd.AddCommand(buildLifecycleCmd("restart"))
}
