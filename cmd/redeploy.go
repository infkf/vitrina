package cmd

import (
	"fmt"
	"path/filepath"

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
)

func init() {
	redeployCmd.Flags().StringVar(&redeployBranch, "branch", "", "Checkout this branch before rebuilding")
	redeployCmd.Flags().StringVar(&redeployTag, "tag", "", "Checkout this tag before rebuilding")
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
	} else {
		fmt.Println("Pulling latest...")
		if err := deploy.PullLatest(appDir); err != nil {
			return fmt.Errorf("git pull failed: %w", err)
		}
	}

	fmt.Println("Rebuilding containers...")
	if err := deploy.ComposeUp(appDir); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	fmt.Printf("Redeployed: https://%s\n", app.FQDN)
	return nil
}
