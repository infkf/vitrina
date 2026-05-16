package cmd

import (
	"fmt"
	"os"

	"vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var remoteName string

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
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
		if remoteName != "" {
			return remote.Execute(remoteName, cmd, args)
		}
		return fn(cmd, args)
	}
}
