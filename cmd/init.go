package cmd

import (
	"fmt"

	"vitrina/internal/caddy"
	"vitrina/internal/config"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init --domain <domain> --email <email>",
	Short: "Initialize Vitrina on this server",
	Long: `Creates the directory structure, writes the main Caddyfile,
and stores the base configuration.

Must be run once before any other commands.`,
	RunE: runRemoteOrLocal(runInit),
}

var (
	initDomain string
	initEmail  string
)

func init() {
	initCmd.Flags().StringVarP(&initDomain, "domain", "d", "", "Base domain (e.g. mydomain.com)")
	initCmd.Flags().StringVarP(&initEmail, "email", "e", "", "Email for Let's Encrypt notifications")
	initCmd.MarkFlagRequired("domain")
	initCmd.MarkFlagRequired("email")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	if config.Exists() {
		return fmt.Errorf("vitrina is already initialized (config exists at %s)\nDelete it first if you want to re-initialize", config.Path())
	}

	cfg := &config.Config{
		Domain:       initDomain,
		Email:        initEmail,
		CaddyConfDir: config.DefaultCaddyConfD,
		AppsDir:      config.DefaultAppsDir,
	}

	if err := config.Save(cfg); err != nil {
		return err
	}

	if err := caddy.WriteMainCaddyfile(initEmail); err != nil {
		return err
	}

	fmt.Println("Vitrina initialized successfully.")
	fmt.Printf("  Config:       %s\n", config.Path())
	fmt.Printf("  Caddyfile:    /etc/caddy/Caddyfile\n")
	fmt.Printf("  App snippets: %s/\n\n", config.DefaultCaddyConfD)
	fmt.Println("Next steps:")
	fmt.Println("  1. Review /etc/caddy/Caddyfile")
	fmt.Println("  2. Start Caddy:   systemctl start caddy")
	fmt.Println("  3. Add your apps: vitrina add <subdomain> <port>")

	return nil
}
