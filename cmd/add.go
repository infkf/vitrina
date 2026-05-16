package cmd

import (
	"fmt"
	"regexp"
	"strconv"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/registry"
	"vitrina/internal/scaffold"

	"github.com/spf13/cobra"
)

var subdomainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

var addCmd = &cobra.Command{
	Use:   "add <subdomain> <port>",
	Short: "Register a new app and wire it into the proxy",
	Long: `Creates a Caddy routing rule so <subdomain>.<domain> traffic
is reverse-proxied to localhost:<port>.

Optionally generates deployment boilerplate (docker-compose or systemd).`,
	Args: cobra.ExactArgs(2),
	RunE: runRemoteOrLocal(runAdd),
}

var (
	addScaffoldType string
)

func init() {
	addCmd.Flags().StringVarP(&addScaffoldType, "scaffold", "s", "none",
		"Generate boilerplate: docker, systemd, or none")
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	subdomain := args[0]
	portStr := args[1]

	if !subdomainRegex.MatchString(subdomain) {
		return fmt.Errorf("invalid subdomain %q: must be alphanumeric with optional hyphens, max 63 chars", subdomain)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %q: must be an integer between 1 and 65535", portStr)
	}

	if port == 80 || port == 443 {
		return fmt.Errorf("port %d is reserved for HTTP/HTTPS — choose an internal port", port)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fqdn := fmt.Sprintf("%s.%s", subdomain, cfg.Domain)

	store := registry.New()

	app := &registry.App{
		Subdomain: subdomain,
		FQDN:      fqdn,
		Port:      port,
		Scaffold:  addScaffoldType,
	}

	if err := caddy.WriteAppConfig(cfg, fqdn, port); err != nil {
		return fmt.Errorf("failed to write caddy config: %w", err)
	}

	if err := caddy.Validate(); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		return fmt.Errorf("caddy validation failed — changes rolled back:\n%w", err)
	}

	if err := store.Add(app); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		return fmt.Errorf("registration failed — caddy config rolled back: %w", err)
	}

	if err := caddy.Reload(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Warning: app registered but Caddy reload failed:\n  %v\n"+
				"Run 'vitrina list' to verify, then reload Caddy manually.\n", err)
		return nil
	}

	fmt.Printf("Registered: %s -> localhost:%d\n", fqdn, port)

	if addScaffoldType != "none" {
		si := scaffold.AppInfo{Subdomain: subdomain, Port: port}
		if err := scaffold.Generate(cfg.AppsDir, addScaffoldType, si); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: scaffold generation failed: %v\n", err)
		}
	}

	return nil
}
