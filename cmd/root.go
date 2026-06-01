package cmd

import (
	"fmt"
	"os"

	"vitrina/internal/output"
	"vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var remoteName string
var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "vitrina",
	Short: "Lightweight PaaS-lite toolbox for single-VPS app hosting",
	Long: `Vitrina manages web app routing on a single Linux VPS using Caddy
as a reverse proxy with automatic TLS.

Each app is reachable via its own subdomain. Vitrina keeps the Caddy
configuration modular so a bad config from one app never breaks others.

Remote execution:
  Use -r <name> to run any command on a remote VPS.
  Configure remotes with: vitrina remote set <name> --host <ip>`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&remoteName, "remote", "r", "",
		"Execute on remote VPS (configure with 'vitrina remote set')")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false,
		"Output structured JSON instead of human-readable text")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		output.Error(err.Error())
		os.Exit(1)
	}
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command requires root privileges (run with sudo)")
	}
	return nil
}

func runRemoteOrLocal(fn func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		name := remoteName
		if name == "" {
			if cfg, err := remote.Load(); err == nil && cfg.Default != "" {
				name = cfg.Default
			}
		}

		if name != "" {
			return remote.Execute(name, cmd, args)
		}
		return fn(cmd, args)
	}
}
