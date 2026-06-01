package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
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
	redeployBranch string
	redeployTag    string
	redeployQuiet  bool
	redeployForce  bool
)

func init() {
	redeployCmd.Flags().StringVar(&redeployBranch, "branch", "", "Checkout this branch before rebuilding")
	redeployCmd.Flags().StringVar(&redeployTag, "tag", "", "Checkout this tag before rebuilding")
	redeployCmd.Flags().BoolVarP(&redeployQuiet, "quiet", "q", false, "Suppress build output; print a timing summary instead")
	redeployCmd.Flags().BoolVar(&redeployForce, "force", false, "Sync via git fetch + reset --hard (handles force-pushed branches)")
	rootCmd.AddCommand(redeployCmd)
}

func runRedeploy(_ *cobra.Command, args []string) error {
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
		if err := deploy.CheckoutBranch(appDir, redeployBranch); err != nil {
			return fmt.Errorf("checkout branch failed: %w", err)
		}
	} else if redeployTag != "" {
		fmt.Printf("Checking out tag %q...\n", redeployTag)
		if err := deploy.CheckoutTag(appDir, redeployTag); err != nil {
			return fmt.Errorf("checkout tag failed: %w", err)
		}
	} else if redeployForce {
		if err := deploy.ForcePull(appDir, app.GitRef, redeployQuiet); err != nil {
			return fmt.Errorf("force sync failed: %w", err)
		}
	} else {
		fmt.Println("Pulling latest...")
		if err := deploy.PullLatest(appDir, app.GitRef, redeployQuiet); err != nil {
			return fmt.Errorf("git pull failed: %w", err)
		}
	}

	if deploy.HasDockerCompose(appDir) {
		fmt.Println("Found docker-compose.yml — using it, injecting PORT via .env")
		if err := deploy.WriteEnvFile(appDir, app.Port); err != nil {
			return fmt.Errorf("failed to write .env: %w", err)
		}
	} else {
		pf, _ := deploy.ParseProcfile(appDir)
		fmt.Println("Regenerating compose file...")
		if err := deploy.WriteCompose(appDir, app.Subdomain, app.Port, pf); err != nil {
			return fmt.Errorf("failed to regenerate compose: %w", err)
		}
	}

	fmt.Println("Rebuilding containers...")
	if err := deploy.ComposeUp(appDir, redeployQuiet); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	app.LastDeployedAt = time.Now().UTC().Format(time.RFC3339)
	if redeployBranch != "" {
		app.GitRef = redeployBranch
	} else if redeployTag != "" {
		app.GitRef = redeployTag
	}
	if err := store.Update(app); err != nil {
		fmt.Printf("Warning: failed to update app metadata: %v\n", err)
	}

	fmt.Printf("Redeployed: https://%s\n", app.FQDN)
	return nil
}
