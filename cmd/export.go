package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vitrina/internal/config"
	"vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export [output.tar.gz]",
	Short: "Export Vitrina state to a tarball",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
}

func runExport(cmd *cobra.Command, args []string) error {
	outPath := "vitrina-export.tar.gz"
	if len(args) == 1 {
		outPath = args[0]
	}
	absOut, err := filepath.Abs(outPath)
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
		return runLocalExport(absOut)
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

	fmt.Printf("Generating export on %s...\n", name)
	remoteOut := "/tmp/vitrina-export.tar.gz"
	
	script := fmt.Sprintf(`%s export %s`, r.VitrinaPath, remoteOut)
	if err := remote.RunScript(r, script); err != nil {
		return fmt.Errorf("remote export failed: %w", err)
	}

	fmt.Printf("Downloading export to %s...\n", outPath)
	if err := remote.DownloadFile(r, remoteOut, absOut); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	_ = remote.RunCommand(r, fmt.Sprintf("rm %s", remoteOut))

	return nil
}

func runLocalExport(outPath string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create export file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	addFileToTar := func(sourcePath, tarPath string) error {
		info, err := os.Stat(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		header.Name = tarPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		file, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	}

	addDirToTar := func(sourceDir, tarPrefix string) error {
		return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() {
				return nil
			}

			if strings.HasSuffix(info.Name(), ".caddy") || info.Name() == ".env" {
				rel, _ := filepath.Rel(sourceDir, path)
				return addFileToTar(path, filepath.Join(tarPrefix, rel))
			}
			return nil
		})
	}

	fmt.Printf("Exporting Vitrina state locally to %s...\n", outPath)

	_ = addFileToTar(config.Path(), "config.json")
	_ = addFileToTar(filepath.Join(config.DefaultConfigDir, config.DefaultAppsFile), "apps.json")
	_ = addDirToTar(cfg.CaddyConfDir, "caddy")
	_ = addDirToTar(cfg.AppsDir, "apps")

	fmt.Println("Export complete.")
	return nil
}
