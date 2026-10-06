package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infkf/vitrina/internal/atomicfile"
	"github.com/infkf/vitrina/internal/clierror"
	"github.com/spf13/cobra"
)

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// Quote returns a shell-safe single-quoted argument for scripts sent over SSH.
func Quote(s string) string { return shellEscape(s) }

type Remote struct {
	Host         string `json:"host"`
	User         string `json:"user,omitempty"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
	UseSudo      bool   `json:"use_sudo,omitempty"`
	VitrinaPath  string `json:"vitrina_path,omitempty"`
	Domain       string `json:"domain,omitempty"`
	Email        string `json:"email,omitempty"`
}

type Config struct {
	Remotes map[string]*Remote `json:"remotes"`
	Default string             `json:"default,omitempty"`
}

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = "/root"
	}
	return filepath.Join(home, ".vitrina")
}

func configPath() string {
	return filepath.Join(configDir(), "remotes.json")
}

func Load() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Remotes: make(map[string]*Remote)}, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", configPath(), err)
	}
	if len(data) == 0 {
		return &Config{Remotes: make(map[string]*Remote)}, nil
	}
	c := &Config{Remotes: make(map[string]*Remote)}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", configPath(), err)
	}
	if c.Remotes == nil {
		c.Remotes = make(map[string]*Remote)
	}
	return c, nil
}

func Save(c *Config) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return fmt.Errorf("failed to create %s: %w", configDir(), err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(configPath(), data, 0600)
}

func (r *Remote) ApplyDefaults() {
	if r.User == "" {
		r.User = "root"
	}
	if r.Port == 0 {
		r.Port = 22
	}
	if r.VitrinaPath == "" {
		r.VitrinaPath = "/usr/local/bin/vitrina"
	}
}

func Execute(name string, cmd *cobra.Command, args []string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	r, ok := cfg.Remotes[name]
	if !ok {
		names := make([]string, 0, len(cfg.Remotes))
		for k := range cfg.Remotes {
			names = append(names, k)
		}
		sort.Strings(names)
		if len(names) == 0 {
			return fmt.Errorf("no remotes configured — use 'vitrina remote set %s --host <ip>' first", name)
		}
		return fmt.Errorf("remote %q not found (known: %s)", name, strings.Join(names, ", "))
	}

	r.ApplyDefaults()

	// Resolve the direct-child-of-root subcommand name.
	// For top-level commands (deploy, list, etc.) this is cmd.Name().
	// For nested commands (env set, env list) this is the parent (env).
	topCmd := resolveTopLevelCmd(cmd)

	remoteArgs := filterRemoteArgs(topCmd)
	remoteCmd := buildRemoteCommand(r, topCmd, remoteArgs)

	sshArgs := buildSSHArgs(r, remoteCmd)

	sshCmd := exec.CommandContext(cmd.Context(), "ssh", sshArgs...)
	sshCmd.Stdin = nil
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	return wrapCommandError(sshCmd.Run())
}

// RunScript pipes script to bash over SSH, applying sudo when configured.
func RunScript(r *Remote, script string) error {
	return RunScriptContext(context.Background(), r, script)
}

func RunScriptContext(ctx context.Context, r *Remote, script string) error {
	command := "bash -s"
	if r.UseSudo && r.User != "root" {
		command = "sudo " + command
	}
	args := buildSSHArgs(r, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return wrapCommandError(cmd.Run())
}

// RunCommand runs a single shell command over SSH, applying sudo when configured.
func RunCommand(r *Remote, command string) error {
	return RunCommandContext(context.Background(), r, command)
}

func RunCommandContext(ctx context.Context, r *Remote, command string) error {
	if r.UseSudo && r.User != "root" {
		command = "sudo " + command
	}
	args := buildSSHArgs(r, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return wrapCommandError(cmd.Run())
}

// RunCommandCaptured is like RunCommand but returns combined stdout+stderr.
func RunCommandCaptured(r *Remote, command string) (string, error) {
	return RunCommandCapturedContext(context.Background(), r, command)
}

func RunCommandCapturedContext(ctx context.Context, r *Remote, command string) (string, error) {
	if r.UseSudo && r.User != "root" {
		command = "sudo " + command
	}
	args := buildSSHArgs(r, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), wrapCommandError(err)
}

// UploadFile copies localPath to remotePath on r via SCP.
func UploadFile(r *Remote, localPath, remotePath string) error {
	return UploadFileContext(context.Background(), r, localPath, remotePath)
}

func UploadFileContext(ctx context.Context, r *Remote, localPath, remotePath string) error {
	args := buildSCPArgs(r)
	args = append(args, localPath, fmt.Sprintf("%s@%s:%s", r.User, r.Host, remotePath))
	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return wrapCommandError(cmd.Run())
}

func buildSCPArgs(r *Remote) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectionAttempts=3",
		"-P", fmt.Sprintf("%d", r.Port),
	}
	if r.IdentityFile != "" {
		idFile := r.IdentityFile
		if strings.HasPrefix(idFile, "~/") {
			home, _ := os.UserHomeDir()
			idFile = filepath.Join(home, idFile[2:])
		}
		args = append(args, "-i", idFile)
	}
	return args
}

// resolveTopLevelCmd walks up the cobra command tree to find the
// direct child of root.  For nested commands like 'env set', this
// returns "env" so the remote invocation packs the full subcommand
// chain into arguments.
func resolveTopLevelCmd(cmd *cobra.Command) string {
	for cmd.Parent() != nil && cmd.Parent().Parent() != nil {
		cmd = cmd.Parent()
	}
	return cmd.Name()
}

func filterRemoteArgs(subcommand string) []string {
	return filterArgs(os.Args[1:], subcommand)
}

func filterArgs(raw []string, subcommand string) []string {
	var result []string
	skipNext := false
	skippedSubcmd := false
	for _, arg := range raw {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "-r" || arg == "--remote" {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "-r") && arg != "-r" {
			continue
		}
		if strings.HasPrefix(arg, "--remote=") {
			continue
		}
		if !skippedSubcmd && arg == subcommand {
			skippedSubcmd = true
			continue
		}
		result = append(result, arg)
	}
	return result
}

func buildRemoteCommand(r *Remote, subcmd string, args []string) string {
	parts := []string{shellEscape(r.VitrinaPath), shellEscape(subcmd)}
	for _, a := range args {
		parts = append(parts, shellEscape(a))
	}
	cmdStr := strings.Join(parts, " ")
	if r.UseSudo && r.User != "root" {
		cmdStr = "sudo " + cmdStr
	}
	return cmdStr
}

func buildSSHArgs(r *Remote, remoteCmd string) []string {
	args := []string{
		"-q",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectionAttempts=3",
		"-p", fmt.Sprintf("%d", r.Port),
	}

	if r.IdentityFile != "" {
		idFile := r.IdentityFile
		if strings.HasPrefix(idFile, "~/") {
			home, _ := os.UserHomeDir()
			idFile = filepath.Join(home, idFile[2:])
		}
		args = append(args, "-i", idFile)
	}

	args = append(args, fmt.Sprintf("%s@%s", r.User, r.Host), remoteCmd)
	return args
}

// DownloadFile uses scp to download a remote file to a local path.
func DownloadFile(r *Remote, remotePath, localPath string) error {
	return DownloadFileContext(context.Background(), r, remotePath, localPath)
}

func DownloadFileContext(ctx context.Context, r *Remote, remotePath, localPath string) error {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "ConnectTimeout=10",
	}
	if r.IdentityFile != "" {
		idFile := r.IdentityFile
		if strings.HasPrefix(idFile, "~/") {
			home, _ := os.UserHomeDir()
			idFile = filepath.Join(home, idFile[2:])
		}
		args = append(args, "-i", idFile)
	}
	args = append(args, "-P", fmt.Sprintf("%d", r.Port))

	target := fmt.Sprintf("%s@%s:%s", r.User, r.Host, remotePath)
	args = append(args, target, localPath)

	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return wrapCommandError(cmd.Run())
}

func wrapCommandError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &clierror.ClassifiedError{Code: clierror.CodeSSHTimeout, Category: "transport", Retryable: true, Err: fmt.Errorf("SSH connection timed out: %w", err)}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 255 {
			return &clierror.ClassifiedError{Code: clierror.CodeSSHUnavailable, Category: "transport", Retryable: true, ExitStatus: 69, Err: err}
		}
		return &clierror.ExitError{Code: clierror.CodeRemoteCommand, ExitStatus: exitErr.ExitCode(), Err: err}
	}
	return &clierror.ClassifiedError{Code: clierror.CodeSSHUnavailable, Category: "transport", Retryable: true, ExitStatus: 69, Err: err}
}
