package cmd

import (
	"fmt"
	"net"
	"os"
	"text/tabwriter"
	"time"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all registered apps and their proxy status",
	Long: `Displays a table of every app managed by Vitrina: subdomain,
FQDN, internal port, and whether the routing snippet is in place.

Use --health to also check whether each app's port is accepting connections.`,
	Args: cobra.NoArgs,
	RunE: runRemoteOrLocal(runList),
}

var listHealth bool

func init() {
	listCmd.Flags().BoolVar(&listHealth, "health", false,
		"Check whether each app's port is accepting TCP connections")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()
	apps, err := store.List()
	if err != nil {
		return err
	}

	if len(apps) == 0 {
		fmt.Println("No apps registered. Use 'vitrina add <subdomain> <port>' to get started.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SUBDOMAIN\tFQDN\tPORT\tROUTING\tHEALTH")
	fmt.Fprintln(w, "---\t---\t---\t---\t---")

	for _, app := range apps {
		routing := routingStatus(cfg, app)
		health := healthStatus(app, listHealth)
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
			app.Subdomain, app.FQDN, app.Port, routing, health)
	}

	return w.Flush()
}

func routingStatus(cfg *config.Config, app *registry.App) string {
	if caddy.AppConfigExists(cfg, app.FQDN) {
		return "configured"
	}
	return "missing-snippet"
}

func healthStatus(app *registry.App, check bool) string {
	if !check {
		return "-"
	}
	addr := fmt.Sprintf("127.0.0.1:%d", app.Port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "down"
	}
	conn.Close()
	return "reachable"
}
