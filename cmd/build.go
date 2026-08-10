package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func buildLDFlags(srcDir string) string {
	version := "dev"
	commit := "unknown"
	if gitOut, err := execGit(srcDir, "describe", "--tags", "--always", "--dirty"); err == nil {
		version = gitOut
	}
	if gitOut, err := execGit(srcDir, "rev-parse", "--short", "HEAD"); err == nil {
		commit = gitOut
	}
	buildTime := time.Now().UTC().Format(time.RFC3339)

	return fmt.Sprintf(
		"-X github.com/infkf/vitrina/internal/version.Version=%s -X github.com/infkf/vitrina/internal/version.Commit=%s -X github.com/infkf/vitrina/internal/version.BuildTime=%s",
		version, commit, buildTime,
	)
}

func execGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
