package cmd

import (
	"path/filepath"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <subdomain>",
	Short: "Stream container logs for a deployed app",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteOrLocal(runLogs),
}

var logsFollow bool

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(_ *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()
	app, err := store.Get(args[0])
	if err != nil {
		return err
	}

	return deploy.ComposeLogs(filepath.Join(cfg.AppsDir, app.Subdomain), logsFollow)
}
