package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/infkf/vitrina/internal/output"
	"github.com/infkf/vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the Vitrina binary (local or remote)",
	Long: `Rebuilds the Vitrina binary from the current source tree and replaces
the existing binary.

Without -r: upgrades the local vitrina binary in-place.
With -r: cross-compiles a Linux binary (if needed), uploads it to the
remote VPS, and replaces the remote vitrina binary.

Use --binary to skip the build step and provide a pre-built binary.`,
	Args: cobra.NoArgs,
	RunE: runUpgrade,
}

var upgradeBinary string

func init() {
	upgradeCmd.Flags().StringVar(&upgradeBinary, "binary", "",
		"Path to pre-built vitrina binary (skips the build step)")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	name := remoteName
	if name == "" {
		if cfg, err := remote.Load(); err == nil && cfg.Default != "" {
			name = cfg.Default
		}
	}

	if name != "" {
		return runRemoteUpgrade(name)
	}
	return runLocalUpgrade()
}

func runLocalUpgrade() error {
	if err := requireRoot(); err != nil {
		return err
	}

	targetPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine current binary path: %w", err)
	}

	binaryPath, isTemp, err := resolveBinary(targetPath, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if isTemp {
		defer os.Remove(binaryPath)
	}

	fmt.Printf("Upgrading local vitrina at %s...\n", targetPath)

	if err := replaceBinary(binaryPath, targetPath); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	output.Successf("Upgraded: %s", targetPath)
	_ = recordInstalledVersion()
	return nil
}

func runRemoteUpgrade(remoteName string) error {
	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	r, ok := cfg.Remotes[remoteName]
	if !ok {
		return fmt.Errorf("remote %q not found", remoteName)
	}
	r.ApplyDefaults()

	binaryPath, isTemp, err := resolveBinary(r.VitrinaPath, "linux", "amd64")
	if err != nil {
		return err
	}
	if isTemp {
		defer os.Remove(binaryPath)
	}

	fmt.Printf("Upgrading vitrina on %s (%s)...\n", remoteName, r.Host)

	fmt.Println("Uploading binary...")
	if err := remote.UploadFile(r, binaryPath, "/tmp/vitrina"); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	installCmd := fmt.Sprintf("install -m 0755 /tmp/vitrina %s", r.VitrinaPath)
	if err := remote.RunCommand(r, installCmd); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	_ = remote.RunCommand(r, "rm /tmp/vitrina")

	_ = remote.RunCommand(r, fmt.Sprintf("%s __record-version", r.VitrinaPath))

	verifyCmd := fmt.Sprintf("%s --version", r.VitrinaPath)
	out, err := remote.RunCommandCaptured(r, verifyCmd)
	if err != nil {
		output.Warnf("binary replaced but verification failed: %v", err)
	} else {
		fmt.Printf("Verified: %s\n", out)
	}

	output.Successf("Upgraded vitrina on %s", remoteName)
	return nil
}

func resolveBinary(targetPath, targetOS, targetArch string) (string, bool, error) {
	if upgradeBinary != "" {
		return upgradeBinary, false, nil
	}

	if runtime.GOOS == targetOS && runtime.GOARCH == targetArch {
		execPath, err := os.Executable()
		if err != nil {
			return "", false, fmt.Errorf("could not locate current executable: %w", err)
		}
		return execPath, false, nil
	}

	built, err := autoBuildBinary(targetOS, targetArch)
	if err != nil {
		return "", false, err
	}
	return built, true, nil
}

func autoBuildBinary(goos, goarch string) (string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return "", fmt.Errorf(
			"'go' not found in PATH — cross-compile manually:\n"+
				"  GOOS=%s GOARCH=%s go build -o vitrina .\n"+
				"  vitrina upgrade --binary ./vitrina",
			goos, goarch,
		)
	}

	srcDir, err := findGoModDir()
	if err != nil {
		return "", fmt.Errorf(
			"could not find go.mod — cross-compile manually:\n"+
				"  GOOS=%s GOARCH=%s go build -o vitrina .\n"+
				"  vitrina upgrade --binary ./vitrina",
			goos, goarch,
		)
	}

	tmp, err := os.CreateTemp("", "vitrina-*")
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	tmp.Close()

	timer := output.StartTimer(fmt.Sprintf("Building %s/%s binary", goos, goarch))
	ldflags := buildLDFlags(srcDir)
	build := exec.Command("go", "build", "-ldflags", ldflags, "-o", tmp.Name(), ".")
	build.Dir = srcDir
	build.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("build failed: %w", err)
	}
	timer.Stop()
	return tmp.Name(), nil
}

func replaceBinary(src, dst string) error {
	resolved, err := filepath.EvalSymlinks(dst)
	if err == nil {
		dst = resolved
	}

	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return err
		}
		tmp := dst + ".new"
		if cpErr := copyFile(src, tmp); cpErr != nil {
			return fmt.Errorf("copy failed: %w", cpErr)
		}
		os.Chmod(tmp, 0755)
		if renameErr := os.Rename(tmp, dst); renameErr != nil {
			os.Remove(tmp)
			return fmt.Errorf("replace failed: %w", renameErr)
		}
		return nil
	}
	os.Chmod(dst, 0755)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
