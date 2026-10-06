package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/deploy"
	"github.com/infkf/vitrina/internal/output"
	"github.com/infkf/vitrina/internal/registry"
	"github.com/infkf/vitrina/internal/remote"

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
	packTimer := output.StartTimer(fmt.Sprintf("Packaging local directory %s", absDir))
	archivePath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-push.tar.gz", subdomain))
	defer os.Remove(archivePath)

	tarArgs := buildTarArgs(archivePath, absDir, true)
	tarCmd := exec.Command("tar", tarArgs...)
	tarCmd.Stdout = os.Stdout
	tarCmd.Stderr = os.Stderr
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	packTimer.Stop()

	// 3. Upload archive
	upTimer := output.StartTimer(fmt.Sprintf("Uploading to %s", name))
	remoteArchivePath := fmt.Sprintf("/tmp/%s-push.tar.gz", subdomain)
	if err := remote.UploadFileContext(cmd.Context(), r, archivePath, remoteArchivePath); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	upTimer.Stop()

	// 4. Run extract and redeploy on remote
	fmt.Println("Deploying on remote...")
	remoteScript := fmt.Sprintf(`
 set -e
if [ ! -d "/etc/vitrina/apps/%s" ]; then
	 echo "App %s is not deployed yet. Please use 'vitrina add %s' and optionally setup docker-compose.yml first."
	 exit 1
fi
app=/etc/vitrina/apps/%s
stage=$(mktemp -d /etc/vitrina/apps/.%s-stage.XXXXXX)
old="${app}.previous"
trap 'rm -rf "$stage"' EXIT
tar -xzf %s -C "$stage" --no-same-owner
if [ -f "$app/.env" ]; then cp "$app/.env" "$stage/.env"; fi
rm -rf "$old"
mv "$app" "$old"
mv "$stage" "$app"
if ! %s redeploy %s; then
  rm -rf "$app"
  mv "$old" "$app"
  exit 1
fi
rm -rf "$old"
rm %s
`, subdomain, subdomain, subdomain, subdomain, subdomain, remoteArchivePath, remote.Quote(r.VitrinaPath), subdomain, remoteArchivePath)

	if err := remote.RunScriptContext(cmd.Context(), r, remoteScript); err != nil {
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
	copyTimer := output.StartTimer("Copying files")
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

	stageDir, err := os.MkdirTemp(filepath.Dir(appDir), ".push-stage-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stageDir)

	extractCmd := exec.Command("tar", "-xf", archivePath, "-C", stageDir, "--no-same-owner")
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("failed to extract local archive: %w", err)
	}
	if envData, err := os.ReadFile(filepath.Join(appDir, ".env")); err == nil {
		if err := os.WriteFile(filepath.Join(stageDir, ".env"), envData, 0600); err != nil {
			return fmt.Errorf("failed to preserve environment: %w", err)
		}
	}
	oldDir := appDir + ".previous"
	_ = os.RemoveAll(oldDir)
	if err := os.Rename(appDir, oldDir); err != nil {
		return fmt.Errorf("failed to stage existing app: %w", err)
	}
	if err := os.Rename(stageDir, appDir); err != nil {
		_ = os.Rename(oldDir, appDir)
		return fmt.Errorf("failed to activate staged app: %w", err)
	}
	copyTimer.Stop()

	fmt.Println("Deploying...")

	if err := deploy.WriteVitrinaMarker(appDir, &deploy.VitrinaMarker{
		Subdomain:  subdomain,
		FQDN:       app.FQDN,
		Port:       app.Port,
		DeployedBy: "push",
		GitURL:     app.GitURL,
		GitRef:     app.GitRef,
	}); err != nil {
		output.Warnf("failed to write .vitrina.json: %v", err)
	}

	cmdStr := os.Args[0]
	if !strings.HasSuffix(cmdStr, "vitrina") {
		cmdStr = "vitrina" // Fallback if os.Args[0] is weird
	}

	cmd := exec.Command(cmdStr, "redeploy", subdomain)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(appDir)
		_ = os.Rename(oldDir, appDir)
		return fmt.Errorf("redeploy failed; restored previous app: %w", err)
	}
	if err := os.RemoveAll(oldDir); err != nil {
		return fmt.Errorf("deployment succeeded but failed to remove previous app: %w", err)
	}
	return nil
}

func buildTarArgs(archivePath, localDir string, compress bool) []string {
	flag := "-cf"
	if compress {
		flag = "-czf"
	}
	args := []string{flag, archivePath, "--exclude=.git", "--exclude=.env", "--exclude=.vitrina.json"}

	ignoreFile := filepath.Join(localDir, ".vitrinaignore")
	if _, err := os.Stat(ignoreFile); err == nil {
		args = append(args, fmt.Sprintf("--exclude-from=%s", ignoreFile))
	}

	args = append(args, "-C", localDir, ".")
	return args
}
