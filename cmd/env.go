package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env <list|set|unset>",
	Short: "Manage environment variables for a deployed app",
	Args:  cobra.NoArgs,
	Run:   func(cmd *cobra.Command, args []string) { cmd.Help() },
}

var envListCmd = &cobra.Command{
	Use:   "list <subdomain>",
	Short: "List environment variables for an app",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteOrLocal(runEnvList),
}

var envSetCmd = &cobra.Command{
	Use:   "set <subdomain> KEY=VALUE [KEY2=VALUE2...]",
	Short: "Set environment variables for an app",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runRemoteOrLocal(runEnvSet),
}

var envUnsetCmd = &cobra.Command{
	Use:   "unset <subdomain> KEY [KEY2...]",
	Short: "Unset environment variables for an app",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runRemoteOrLocal(runEnvUnset),
}

var envApply bool

func init() {
	envSetCmd.Flags().BoolVar(&envApply, "apply", false, "Restart containers after setting vars to apply changes immediately")
	envUnsetCmd.Flags().BoolVar(&envApply, "apply", false, "Restart containers after unsetting vars to apply changes immediately")
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envUnsetCmd)

	rootCmd.AddCommand(envCmd)
}

func runEnvList(cmd *cobra.Command, args []string) error {
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
	if err != nil || app == nil {
		return fmt.Errorf("app %q not found", subdomain)
	}

	appDir := filepath.Join(cfg.AppsDir, subdomain)
	vars, err := deploy.ReadEnvFile(appDir)
	if err != nil {
		return err
	}

	if len(vars) == 0 {
		if jsonOutput {
			fmt.Println("{}")
		} else {
			fmt.Println("No environment variables set.")
		}
		return nil
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(vars)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "KEY\tVALUE")
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s\t%s\n", k, vars[k])
	}
	return w.Flush()
}

func runEnvSet(cmd *cobra.Command, args []string) error {
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
	if err != nil || app == nil {
		return fmt.Errorf("app %q not found", subdomain)
	}

	appDir := filepath.Join(cfg.AppsDir, subdomain)
	vars := make(map[string]string)
	for _, pair := range args[1:] {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format for %q, expected KEY=VALUE", pair)
		}
		if parts[0] == "PORT" {
			return fmt.Errorf("cannot override PORT variable as it is managed by Vitrina")
		}
		vars[parts[0]] = parts[1]
	}

	if err := deploy.SetEnvVars(appDir, vars); err != nil {
		return err
	}

	appVars, _ := deploy.ReadEnvFile(appDir)
	app.EnvKeys = []string{}
	for k := range appVars {
		if k != "PORT" {
			app.EnvKeys = append(app.EnvKeys, k)
		}
	}
	if err := store.Update(app); err != nil {
		fmt.Printf("Warning: failed to update app metadata: %v\n", err)
	}

	if envApply {
		fmt.Println("Restarting containers to apply changes...")
		if err := deploy.ComposeRestart(appDir, false); err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
		fmt.Printf("Environment variables set and applied to %s.\n", subdomain)
	} else {
		fmt.Printf("Environment variables set. Run 'vitrina redeploy %s' to apply changes.\n", subdomain)
	}
	return nil
}

func runEnvUnset(cmd *cobra.Command, args []string) error {
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
	if err != nil || app == nil {
		return fmt.Errorf("app %q not found", subdomain)
	}

	appDir := filepath.Join(cfg.AppsDir, subdomain)
	keys := make([]string, 0, len(args)-1)
	for _, key := range args[1:] {
		if key == "PORT" {
			return fmt.Errorf("cannot unset PORT variable as it is managed by Vitrina")
		}
		keys = append(keys, key)
	}

	if err := deploy.UnsetEnvVars(appDir, keys); err != nil {
		return err
	}

	appVars, _ := deploy.ReadEnvFile(appDir)
	app.EnvKeys = []string{}
	for k := range appVars {
		if k != "PORT" {
			app.EnvKeys = append(app.EnvKeys, k)
		}
	}
	if err := store.Update(app); err != nil {
		fmt.Printf("Warning: failed to update app metadata: %v\n", err)
	}

	if envApply {
		fmt.Println("Restarting containers to apply changes...")
		if err := deploy.ComposeRestart(appDir, false); err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
		fmt.Printf("Environment variables unset and applied to %s.\n", subdomain)
	} else {
		fmt.Printf("Environment variables unset. Run 'vitrina redeploy %s' to apply changes.\n", subdomain)
	}
	return nil
}
