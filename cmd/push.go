package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vitrina/internal/config"
	"vitrina/internal/registry"
	"vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push <subdomain> [local_dir]",
	Short: "Deploy a local directory to a remote VPS",
	Long: `Packages the local directory (ignoring .git), uploads it to the remote,
and deploys it there without needing a git push.

If local_dir is omitted, uses the current directory.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)
}

func runPush(cmd *cobra.Command, args []string) error {
	subdomain := args[0]
	localDir := "."
	if len(args) == 2 {
		localDir = args[1]
	}

	absDir, err := filepath.Abs(localDir)
	if err != nil {
		return err
	}

	// 1. Determine target remote
	name := remoteName
	if name == "" {
		if cfg, err := remote.Load(); err == nil && cfg.Default != "" {
			name = cfg.Default
		}
	}

	if name == "" {
		// Local push (running on the server itself)
		return runLocalPush(subdomain, absDir)
	}

	// Remote push
	cfg, err := remote.Load()
	if err != nil {
		return err
	}
	r, ok := cfg.Remotes[name]
	if !ok {
		return fmt.Errorf("remote %q not found", name)
	}
	r.ApplyDefaults()

	// 2. Package local directory
	fmt.Printf("Packaging local directory %s...\n", absDir)
	archivePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-push.tar.gz", subdomain))
	defer os.Remove(archivePath)

	tarArgs := buildTarArgs(archivePath, absDir, true)
	tarCmd := exec.Command("tar", tarArgs...)
	tarCmd.Stdout = os.Stdout
	tarCmd.Stderr = os.Stderr
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}

	// 3. Upload archive
	fmt.Printf("Uploading to %s...\n", name)
	remoteArchivePath := fmt.Sprintf("/tmp/%s-push.tar.gz", subdomain)
	if err := remote.UploadFile(r, archivePath, remoteArchivePath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// 4. Run extract and redeploy on remote
	fmt.Println("Deploying on remote...")
	remoteScript := fmt.Sprintf(`
set -e
if [ ! -d "/etc/vitrina/apps/%s" ]; then
	echo "App %s is not deployed yet. Please use 'vitrina add %s' and optionally setup docker-compose.yml first."
	exit 1
fi
mkdir -p /etc/vitrina/apps/%s
tar -xzf %s -C /etc/vitrina/apps/%s
rm %s
%s redeploy %s
`, subdomain, subdomain, subdomain, subdomain, remoteArchivePath, subdomain, remoteArchivePath, r.VitrinaPath, subdomain)

	if err := remote.RunScript(r, remoteScript); err != nil {
		return fmt.Errorf("remote deploy failed: %w", err)
	}

	return nil
}

func runLocalPush(subdomain, localDir string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	sysCfg, err := config.Load()
	if err != nil {
		return err
	}

	store := registry.New()
	app, err := store.Get(subdomain)
	if err != nil || app == nil {
		return fmt.Errorf("app %q not found. Run 'vitrina add %s' first.", subdomain, subdomain)
	}

	appDir := filepath.Join(sysCfg.AppsDir, subdomain)

	fmt.Printf("Copying files to %s...\n", appDir)

	archivePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-push.tar", subdomain))
	defer os.Remove(archivePath)

	tarArgs := buildTarArgs(archivePath, localDir, false)
	tarCmd := exec.Command("tar", tarArgs...)
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("failed to create local archive: %w", err)
	}

	if err := os.MkdirAll(appDir, 0755); err != nil {
		return err
	}

	extractCmd := exec.Command("tar", "-xf", archivePath, "-C", appDir)
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("failed to extract local archive: %w", err)
	}

	fmt.Println("Deploying...")

	cmdStr := os.Args[0]
	if !strings.HasSuffix(cmdStr, "vitrina") {
		cmdStr = "vitrina" // Fallback if os.Args[0] is weird
	}

	cmd := exec.Command(cmdStr, "redeploy", subdomain)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildTarArgs(archivePath, localDir string, compress bool) []string {
	flag := "-cf"
	if compress {
		flag = "-czf"
	}
	args := []string{flag, archivePath, "--exclude=.git"}

	ignoreFile := filepath.Join(localDir, ".vitrinaignore")
	if _, err := os.Stat(ignoreFile); err == nil {
		args = append(args, fmt.Sprintf("--exclude-from=%s", ignoreFile))
	}

	args = append(args, "-C", localDir, ".")
	return args
}
