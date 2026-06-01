package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"time"

	"vitrina/internal/caddy"
	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy <subdomain> <git-url>",
	Short: "Clone and deploy an app from a git repository",
	Long: `Clones the repository, wires it into the reverse proxy, and starts containers.

Source priority (first match wins):
  1. Repo's own docker-compose.yml — used as-is; PORT injected via .env
  2. Procfile                      — multi-service compose generated
  3. Neither                       — single-service compose generated

Only the web process gets a Caddy route. Workers run internally.
The app is reachable at https://<subdomain>.<domain> once containers start.`,
	Args: cobra.ExactArgs(2),
	RunE: runRemoteOrLocal(runDeploy),
}

var (
	deployBranch string
	deployTag    string
	deployQuiet  bool
)

func init() {
	deployCmd.Flags().StringVar(&deployBranch, "branch", "", "Checkout this branch after cloning")
	deployCmd.Flags().StringVar(&deployTag, "tag", "", "Checkout this tag after cloning")
	deployCmd.Flags().BoolVarP(&deployQuiet, "quiet", "q", false, "Suppress build output; print a timing summary instead")
	rootCmd.AddCommand(deployCmd)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	subdomain, repoURL := args[0], args[1]

	if !subdomainRegex.MatchString(subdomain) {
		return fmt.Errorf("invalid subdomain %q: must be alphanumeric with optional hyphens, max 63 chars", subdomain)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()

	if existing, err := store.Get(subdomain); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("failed to check registry: %w", err)
		}
	} else if existing != nil {
		return fmt.Errorf("subdomain %q already deployed — use 'vitrina redeploy %s' to update", subdomain, subdomain)
	}

	port, err := store.NextFreePort(3000)
	if err != nil {
		return err
	}

	appDir := filepath.Join(cfg.AppsDir, subdomain)

	fmt.Printf("Cloning %s...\n", repoURL)
	if err := deploy.CloneRepo(repoURL, appDir, deployQuiet); err != nil {
		return fmt.Errorf("clone failed: %w", err)
	}

	if deployBranch != "" {
		fmt.Printf("Checking out branch %q...\n", deployBranch)
		if err := deploy.CheckoutBranch(appDir, deployBranch); err != nil {
			os.RemoveAll(appDir)
			return fmt.Errorf("checkout branch failed: %w", err)
		}
	} else if deployTag != "" {
		fmt.Printf("Checking out tag %q...\n", deployTag)
		if err := deploy.CheckoutTag(appDir, deployTag); err != nil {
			os.RemoveAll(appDir)
			return fmt.Errorf("checkout tag failed: %w", err)
		}
	}

	if deploy.HasDockerCompose(appDir) {
		fmt.Println("Found docker-compose.yml — using it, injecting PORT via .env")
		if err := deploy.WriteEnvFile(appDir, port); err != nil {
			os.RemoveAll(appDir)
			return fmt.Errorf("failed to write .env: %w", err)
		}
	} else {
		pf, err := deploy.ParseProcfile(appDir)
		if err != nil {
			os.RemoveAll(appDir)
			return fmt.Errorf("failed to read Procfile: %w", err)
		}
		if len(pf) == 0 && !deploy.HasDockerfile(appDir) {
			os.RemoveAll(appDir)
			return fmt.Errorf("repo has no Dockerfile, Procfile, or docker-compose.yml\n" +
				"Add a Dockerfile so vitrina can build the image, or use 'vitrina add' for non-containerized apps")
		}
		if len(pf) > 0 {
			fmt.Printf("Found Procfile with %d process types — generating docker-compose.yml\n", len(pf))
		} else {
			fmt.Println("Found Dockerfile — generating single-service docker-compose.yml")
		}
		if err := deploy.WriteCompose(appDir, subdomain, port, pf); err != nil {
			os.RemoveAll(appDir)
			return fmt.Errorf("failed to generate docker-compose.yml: %w", err)
		}
	}

	fqdn := fmt.Sprintf("%s.%s", subdomain, cfg.Domain)

	if err := caddy.WriteAppConfig(cfg, fqdn, port); err != nil {
		os.RemoveAll(appDir)
		return fmt.Errorf("failed to write caddy config: %w", err)
	}
	if err := caddy.Validate(); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		os.RemoveAll(appDir)
		return fmt.Errorf("caddy validation failed — changes rolled back:\n%w", err)
	}

	app := &registry.App{
		Subdomain:      subdomain,
		FQDN:           fqdn,
		Port:           port,
		Scaffold:       "docker",
		GitURL:         repoURL,
		GitRef:         deployBranch,
		LastDeployedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if deployTag != "" {
		app.GitRef = deployTag
	}
	if err := store.Add(app); err != nil {
		caddy.RemoveAppConfig(cfg, fqdn)
		os.RemoveAll(appDir)
		return fmt.Errorf("registration failed — caddy config rolled back: %w", err)
	}

	fmt.Printf("Starting containers (port %d)...\n", port)
	if err := deploy.ComposeUp(appDir, deployQuiet); err != nil {
		_ = deploy.ComposeCommand(appDir, "down", "-v")
		store.Remove(subdomain)
		caddy.RemoveAppConfig(cfg, fqdn)
		os.RemoveAll(appDir)
		return fmt.Errorf("docker compose up failed — changes rolled back: %w", err)
	}

	if err := caddy.Reload(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Warning: app deployed but Caddy reload failed:\n  %v\n"+
				"Run 'vitrina list' to verify, then reload Caddy manually.\n", err)
		return nil
	}

	fmt.Printf("Deployed: https://%s\n", fqdn)
	return nil
}
