package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type Remote struct {
	Host         string `json:"host"`
	User         string `json:"user,omitempty"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
	UseSudo      bool   `json:"use_sudo,omitempty"`
	VitrinaPath  string `json:"vitrina_path,omitempty"`
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
	return os.WriteFile(configPath(), data, 0600)
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

	remoteArgs := filterRemoteArgs()
	remoteCmd := buildRemoteCommand(r, cmd.Name(), remoteArgs)

	sshArgs := buildSSHArgs(r, remoteCmd)

	sshCmd := exec.Command("ssh", sshArgs...)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	return sshCmd.Run()
}

func filterRemoteArgs() []string {
	var result []string
	skipNext := false
	for _, arg := range os.Args[1:] {
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
		"-o", "PasswordAuthentication=no",
		"-o", "ConnectTimeout=10",
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
