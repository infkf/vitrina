package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/output"
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
var listPublic bool

func init() {
	listCmd.Flags().BoolVar(&listHealth, "health", false,
		"Check whether each app's port is accepting TCP connections")
	listCmd.Flags().BoolVar(&listPublic, "public", false,
		"Check health via public HTTPS FQDN (implies --health)")
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

	type listOut struct {
		App     *registry.App `json:"app"`
		Routing string        `json:"routing"`
		Health  string        `json:"health"`
	}

	var results []listOut
	for _, app := range apps {
		results = append(results, listOut{
			App:     app,
			Routing: routingStatus(cfg, app),
			Health:  healthStatus(app, listHealth || listPublic, listPublic),
		})
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	if len(apps) == 0 {
		fmt.Println("No apps registered. Use 'vitrina add <subdomain> <port>' to get started.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SUBDOMAIN\tFQDN\tPORT\tROUTING\tHEALTH")
	fmt.Fprintln(w, "---\t---\t---\t---\t---")

	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
			r.App.Subdomain, r.App.FQDN, r.App.Port, r.Routing, r.Health)
	}

	return w.Flush()
}

func routingStatus(cfg *config.Config, app *registry.App) string {
	if caddy.AppConfigExists(cfg, app.FQDN) {
		return "configured"
	}
	return output.HealthStatus("missing-snippet")
}

func healthStatus(app *registry.App, check bool, public bool) string {
	if !check {
		return "-"
	}

	if public {
		return output.HealthStatus(publicHealthStatus(app))
	}

	path := app.HealthPath
	if path == "" {
		path = "/"
	}

	client := http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", app.Port, path)
	resp, err := client.Get(url)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return output.HealthStatus("healthy")
		}
		return output.HealthStatus(fmt.Sprintf("unhealthy (%d)", resp.StatusCode))
	}

	addr := fmt.Sprintf("127.0.0.1:%d", app.Port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return output.HealthStatus("down")
	}
	conn.Close()
	return output.HealthStatus("reachable (tcp)")
}

func publicHealthStatus(app *registry.App) string {
	path := app.HealthPath
	if path == "" {
		path = "/"
	}

	url := fmt.Sprintf("https://%s%s", app.FQDN, path)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Sprintf("unreachable (%v)", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			expiry := resp.TLS.PeerCertificates[0].NotAfter
			daysLeft := int(time.Until(expiry).Hours() / 24)
			return fmt.Sprintf("healthy (TLS %dd)", daysLeft)
		}
		return "healthy (TLS ok)"
	}
	return fmt.Sprintf("unhealthy (%d)", resp.StatusCode)
}
