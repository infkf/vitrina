package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/output"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var redeployCmd = &cobra.Command{
	Use:   "redeploy <subdomain>",
	Short: "Pull latest code and rebuild containers for a deployed app",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteOrLocal(runRedeploy),
}

var (
	redeployBranch        string
	redeployTag           string
	redeployQuiet         bool
	redeployForce         bool
	redeployZeroDowntime  bool
)

func init() {
	redeployCmd.Flags().StringVar(&redeployBranch, "branch", "", "Checkout this branch before rebuilding")
	redeployCmd.Flags().StringVar(&redeployTag, "tag", "", "Checkout this tag before rebuilding")
	redeployCmd.Flags().BoolVarP(&redeployQuiet, "quiet", "q", false, "Suppress build output; print a timing summary instead")
	redeployCmd.Flags().BoolVar(&redeployForce, "force", false, "Sync via git fetch + reset --hard (handles force-pushed branches)")
	redeployCmd.Flags().BoolVarP(&redeployZeroDowntime, "zero-downtime", "z", false, "Deploy new containers on a separate port, swap Caddy, then tear down old")
	rootCmd.AddCommand(redeployCmd)
}

func runRedeploy(cmd *cobra.Command, args []string) error {
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

	if redeployBranch != "" {
		fmt.Printf("Checking out branch %q...\n", redeployBranch)
		if err := deploy.Fetch(appDir, redeployQuiet); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: git fetch failed: %v\n", err)
		}
		if err := deploy.CheckoutBranch(appDir, redeployBranch); err != nil {
			return fmt.Errorf("checkout branch failed: %w", err)
		}
	} else if redeployTag != "" {
		fmt.Printf("Checking out tag %q...\n", redeployTag)
		if err := deploy.Fetch(appDir, redeployQuiet); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: git fetch failed: %v\n", err)
		}
		if err := deploy.CheckoutTag(appDir, redeployTag); err != nil {
			return fmt.Errorf("checkout tag failed: %w", err)
		}
	} else if redeployForce {
		if err := deploy.ForcePull(appDir, app.GitRef, redeployQuiet); err != nil {
			return fmt.Errorf("force sync failed: %w", err)
		}
	} else {
		gitTimer := output.StartTimer("Pulling latest")
		if err := deploy.PullLatest(appDir, app.GitRef, redeployQuiet); err != nil {
			return fmt.Errorf("git pull failed: %w", err)
		}
		gitTimer.Stop()
	}

	if deploy.HasDockerCompose(appDir) {
		fmt.Println("Found docker-compose.yml — using it, injecting PORT via .env")
		if err := deploy.WriteEnvFile(appDir, app.Port); err != nil {
			return fmt.Errorf("failed to write .env: %w", err)
		}
	} else {
		pf, err := deploy.ParseProcfile(appDir)
		if err != nil {
			return fmt.Errorf("failed to read Procfile: %w", err)
		}
		fmt.Println("Regenerating compose file...")
		if err := deploy.WriteCompose(appDir, app.Subdomain, app.Port, pf); err != nil {
			return fmt.Errorf("failed to regenerate compose: %w", err)
		}
	}

	if redeployZeroDowntime {
		greenPort, err := store.NextFreePort(3000)
		if err != nil {
			return fmt.Errorf("failed to assign green port: %w", err)
		}
		if greenPort == app.Port {
			greenPort, err = store.NextFreePort(app.Port + 1)
			if err != nil {
				return fmt.Errorf("failed to assign green port: %w", err)
			}
		}

		newRef := app.GitRef
		if redeployBranch != "" {
			newRef = redeployBranch
		} else if redeployTag != "" {
			newRef = redeployTag
		}

		if err := deploy.BlueGreenDeploy(appDir, app.Subdomain, greenPort, app.Port, redeployQuiet, func() error {
			if err := caddy.WriteAppConfig(cfg, app.FQDN, greenPort); err != nil {
				return fmt.Errorf("caddy config write: %w", err)
			}
			if err := caddy.Validate(); err != nil {
				caddy.WriteAppConfig(cfg, app.FQDN, app.Port)
				return fmt.Errorf("caddy validation: %w", err)
			}
			if err := caddy.Reload(); err != nil {
				caddy.WriteAppConfig(cfg, app.FQDN, app.Port)
				return fmt.Errorf("caddy reload: %w", err)
			}

			app.Port = greenPort
			app.LastDeployedAt = time.Now().UTC().Format(time.RFC3339)
			app.GitRef = newRef
			if err := store.Update(app); err != nil {
				return fmt.Errorf("registry update: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("zero-downtime deploy failed: %w", err)
		}

		output.Successf("Redeployed (zero-downtime): https://%s (port %d)", app.FQDN, greenPort)
		return nil
	}

	buildTimer := output.StartTimer("Rebuilding containers")
	if err := deploy.ComposeUp(appDir, redeployQuiet); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}
	buildTimer.Stop()

	if err := caddy.Reload(); err != nil {
		output.Warnf("Caddy reload failed: %v", err)
	}

	app.LastDeployedAt = time.Now().UTC().Format(time.RFC3339)
	if redeployBranch != "" {
		app.GitRef = redeployBranch
	} else if redeployTag != "" {
		app.GitRef = redeployTag
	}
	if err := store.Update(app); err != nil {
		output.Warnf("failed to update app metadata: %v", err)
	}

	output.Successf("Redeployed: https://%s", app.FQDN)
	return nil
}
