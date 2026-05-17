package cmd

import (
	"vitrina/internal/caddy"

	"github.com/spf13/cobra"
)

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload Caddy to pick up config changes and retry TLS certs",
	Long: `Sends a reload signal to Caddy using the best available method
(caddy reload, systemctl reload, or pkill -USR1).

Use this after updating DNS records so Caddy can provision TLS
certificates for newly-resolvable domains.`,
	Args: cobra.NoArgs,
	RunE: runRemoteOrLocal(runReload),
}

func init() {
	rootCmd.AddCommand(reloadCmd)
}

func runReload(_ *cobra.Command, _ []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	if err := caddy.Reload(); err != nil {
		return err
	}

	return nil
}
