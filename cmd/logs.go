package cmd

import (
	"path/filepath"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <subdomain> [service...]",
	Short: "Stream container logs for a deployed app",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRemoteOrLocal(runLogs),
}

var (
	logsFollow bool
	logsTail   string
	logsSince  string
)

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
	logsCmd.Flags().StringVarP(&logsTail, "tail", "n", "", "Number of lines to show from the end of the logs")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show logs since timestamp (e.g. 2013-01-02T13:23:37) or relative (e.g. 42m for 42 minutes)")
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

	services := args[1:]
	return deploy.ComposeLogs(filepath.Join(cfg.AppsDir, app.Subdomain), logsFollow, logsTail, logsSince, services)
}
