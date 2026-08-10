package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/infkf/vitrina/internal/config"
	"github.com/infkf/vitrina/internal/version"
	"github.com/spf13/cobra"
)

type installedVersion struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.String())
	},
}

var recordVersionCmd = &cobra.Command{
	Use:    "__record-version",
	Hidden: true,
	Short:  "Write current version to /etc/vitrina/version.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		return recordInstalledVersion()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(recordVersionCmd)
}

func recordInstalledVersion() error {
	v := installedVersion{
		Version:   version.Version,
		Commit:    version.Commit,
		BuildTime: version.BuildTime,
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.DefaultConfigDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(config.DefaultConfigDir, "version.json"), data, 0644)
}

func readInstalledVersion() (*installedVersion, error) {
	path := filepath.Join(config.DefaultConfigDir, "version.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v installedVersion
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func printInstalledVersion() {
	v, err := readInstalledVersion()
	if err != nil {
		fmt.Println("Version:   (not recorded)")
		return
	}
	fmt.Printf("Version:    %s (%s) built %s\n", v.Version, v.Commit, v.BuildTime)
}
