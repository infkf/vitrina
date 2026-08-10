package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/infkf/vitrina/internal/caddy"
	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import <input.tar.gz>",
	Short: "Import Vitrina state from a backup",
	Args:  cobra.ExactArgs(1),
	RunE:  runImport,
}

func init() {
	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	inPath := args[0]
	absIn, err := filepath.Abs(inPath)
	if err != nil {
		return err
	}

	name := remoteName
	if name == "" {
		if cfg, err := remote.Load(); err == nil && cfg.Default != "" {
			name = cfg.Default
		}
	}

	if name == "" {
		return runLocalImport(absIn)
	}

	cfg, err := remote.Load()
	if err != nil {
		return err
	}
	r, ok := cfg.Remotes[name]
	if !ok {
		return fmt.Errorf("remote %q not found", name)
	}
	r.ApplyDefaults()

	fmt.Printf("Uploading import to %s...\n", name)
	remoteIn := "/tmp/vitrina-import.tar.gz"
	if err := remote.UploadFile(r, absIn, remoteIn); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	fmt.Printf("Applying import on %s...\n", name)
	script := fmt.Sprintf(`%s import %s`, r.VitrinaPath, remoteIn)
	if err := remote.RunScript(r, script); err != nil {
		return fmt.Errorf("remote import failed: %w", err)
	}

	_ = remote.RunCommand(r, fmt.Sprintf("rm %s", remoteIn))

	return nil
}

func runLocalImport(inPath string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	f, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("failed to open import file: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to read gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	_ = os.MkdirAll(config.DefaultConfigDir, 0755)

	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{
			CaddyConfDir: config.DefaultCaddyConfD,
			AppsDir:      config.DefaultAppsDir,
		}
	}

	fmt.Printf("Importing Vitrina state locally from %s...\n", inPath)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		var destPath string
		if header.Name == "config.json" {
			destPath = config.Path()
		} else if header.Name == "apps.json" {
			destPath = filepath.Join(config.DefaultConfigDir, config.DefaultAppsFile)
		} else if strings.HasPrefix(header.Name, "caddy/") {
			rel := header.Name[len("caddy/"):]
			destPath = filepath.Join(cfg.CaddyConfDir, rel)
			if !isSubPath(cfg.CaddyConfDir, destPath) {
				continue
			}
		} else if strings.HasPrefix(header.Name, "apps/") {
			rel := header.Name[len("apps/"):]
			destPath = filepath.Join(cfg.AppsDir, rel)
			if !isSubPath(cfg.AppsDir, destPath) {
				continue
			}
		} else {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			return err
		}

		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			return err
		}
		outFile.Close()
	}

	fmt.Println("Import complete. Running 'vitrina doctor --heal' to restore state...")
	
	cfg, _ = config.Load()
	if cfg != nil {
		_ = caddy.WriteMainCaddyfile(cfg.Email)
	}

	cmdStr := os.Args[0]
	if !strings.HasSuffix(cmdStr, "vitrina") {
		cmdStr = "vitrina"
	}
	
	healCmd := exec.Command(cmdStr, "doctor", "--heal")
	healCmd.Stdout = os.Stdout
	healCmd.Stderr = os.Stderr
	if err := healCmd.Run(); err != nil {
		return fmt.Errorf("import succeeded but doctor --heal failed (system may be in an inconsistent state): %w", err)
	}

	return nil
}

func isSubPath(base, target string) bool {
	cleanBase := filepath.Clean(base) + string(filepath.Separator)
	cleanTarget := filepath.Clean(target)
	return strings.HasPrefix(cleanTarget, cleanBase)
}
