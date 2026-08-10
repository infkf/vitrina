package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/deploy"
	"github.com/infkf/vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <subdomain> [service...]",
	Short: "Stream container logs for a deployed app",
	Long: `Stream logs from an app's docker compose services.
Use --all to stream logs from all registered apps simultaneously.`,
	Args: cobra.ArbitraryArgs,
	RunE: runRemoteOrLocal(runLogs),
}

var (
	logsFollow bool
	logsTail   string
	logsSince  string
	logsAll    bool
)

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
	logsCmd.Flags().StringVarP(&logsTail, "tail", "n", "", "Number of lines to show from the end")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show logs since timestamp (e.g. 42m for 42 minutes)")
	logsCmd.Flags().BoolVarP(&logsAll, "all", "a", false, "Stream logs from all registered apps (no subdomain required)")
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

	if logsAll {
		return runLogsAll(cfg, store)
	}

	if len(args) == 0 {
		return fmt.Errorf("subdomain is required (or use --all for all apps)")
	}

	app, err := store.Get(args[0])
	if err != nil {
		return err
	}

	services := args[1:]
	return deploy.ComposeLogs(filepath.Join(cfg.AppsDir, app.Subdomain), logsFollow, logsTail, logsSince, services)
}

func runLogsAll(cfg *config.Config, store *registry.Store) error {
	apps, err := store.List()
	if err != nil {
		return err
	}

	type logTarget struct {
		subdomain string
		dir       string
	}

	var targets []logTarget
	for _, app := range apps {
		appDir := filepath.Join(cfg.AppsDir, app.Subdomain)
		if deploy.HasDockerCompose(appDir) || deploy.HasDockerfile(appDir) {
			targets = append(targets, logTarget{subdomain: app.Subdomain, dir: appDir})
		}
	}

	if len(targets) == 0 {
		fmt.Println("No apps with Docker containers registered.")
		return nil
	}

	prefixLen := 0
	for _, t := range targets {
		if len(t.subdomain) > prefixLen {
			prefixLen = len(t.subdomain)
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range targets {
		wg.Add(1)
		go func(subdomain, dir string) {
			defer wg.Done()

			stdout, err := deploy.ComposeLogsPipe(dir, logsFollow, logsTail, logsSince, nil)
			if err != nil {
				mu.Lock()
				fmt.Fprintf(os.Stderr, "%-*s | failed to start: %v\n", prefixLen, subdomain, err)
				mu.Unlock()
				return
			}
			defer stdout.Close()

			prefix := fmt.Sprintf("%-*s | ", prefixLen, subdomain)
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				mu.Lock()
				fmt.Printf("%s%s\n", prefix, line)
				mu.Unlock()
			}
		}(t.subdomain, t.dir)
	}

	wg.Wait()
	return nil
}
