package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/infkf/vitrina/internal/clierror"
	"github.com/infkf/vitrina/internal/output"
	"github.com/infkf/vitrina/internal/protocol"
	"github.com/infkf/vitrina/internal/remote"
	"github.com/infkf/vitrina/internal/version"

	"github.com/spf13/cobra"
)

var remoteName string
var jsonOutput bool
var commandTimeout time.Duration
var nonInteractive bool
var confirmDestructive bool
var dryRun bool

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
	Version:       version.String(),
}

func init() {
	rootCmd.SetVersionTemplate("{{ .Version }}\n")
	rootCmd.PersistentFlags().StringVarP(&remoteName, "remote", "r", "",
		"Execute on remote VPS (configure with 'vitrina remote set')")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", true,
		"Output one structured JSON object (use --json=false for legacy terminal output)")
	rootCmd.PersistentFlags().DurationVar(&commandTimeout, "timeout", 30*time.Minute,
		"Maximum duration for a command and its remote operations")
	rootCmd.PersistentFlags().BoolVar(&nonInteractive, "non-interactive", false,
		"Never prompt; fail with ERR_INTERACTION_REQUIRED when input is required")
	rootCmd.PersistentFlags().BoolVarP(&confirmDestructive, "yes", "y", false,
		"Confirm destructive operations")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false,
		"Plan changes without applying them")
}

func Execute() int {
	started := time.Now().UTC()
	ctx := context.Background()
	if commandTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, commandTimeout)
		defer cancel()
	}
	// Commands historically write progress directly to stdout. Capture it at the
	// protocol boundary so it cannot corrupt the single JSON response.
	tmp, captureErr := os.CreateTemp("", "vitrina-stdout-*")
	if captureErr != nil {
		fmt.Fprintln(os.Stderr, captureErr)
		return 1
	}
	defer os.Remove(tmp.Name())
	originalStdout := os.Stdout
	os.Stdout = tmp
	err := rootCmd.ExecuteContext(ctx)
	_ = tmp.Close()
	os.Stdout = originalStdout
	completed := time.Now().UTC()
	if jsonOutput {
		data := readCaptured(tmp.Name())
		operation := operationName(os.Args[1:])
		response := protocol.Envelope{OK: err == nil, Operation: operation, StartedAt: started, CompletedAt: completed, Data: data, Events: []any{}, Error: nil}
		if err != nil {
			response.Data = nil
			response.Error = clierror.From(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(response)
		if err != nil {
			return clierror.From(err).ExitStatus
		}
		return 0
	}
	if err != nil {
		output.Error(err.Error())
		return clierror.From(err).ExitStatus
	}
	if captured, readErr := os.ReadFile(tmp.Name()); readErr == nil {
		_, _ = originalStdout.Write(captured)
	}
	return 0
}

func readCaptured(name string) any {
	f, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	text := strings.TrimSpace(string(b))
	if text == "" {
		return map[string]any{}
	}
	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		return value
	}
	return text
}

func operationName(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return filepath.Base(arg)
		}
	}
	return "vitrina"
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
