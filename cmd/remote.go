package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/infkf/vitrina/internal/remote"

	"github.com/spf13/cobra"
)

var remoteCmd = &cobra.Command{
	Use:   "remote <set|list|show|default|remove>",
	Short: "Manage remote VPS connections",
	Long: `Configure SSH connections to remote VPS servers running Vitrina.
Once a remote is set, use -r <name> on any command to execute it there.`,
	Args: cobra.NoArgs,
	Run:  func(cmd *cobra.Command, args []string) { cmd.Help() },
}

var remoteSetCmd = &cobra.Command{
	Use:   "set <name> --host <ip-or-hostname>",
	Short: "Add or update a remote VPS",
	Long: `Saves an SSH connection profile for a remote VPS.

Examples:
  vitrina remote set prod --host 203.0.113.5
  vitrina remote set prod --host myvps.example.com --user deploy --port 2222 --key ~/.ssh/vps`,
	Args: cobra.ExactArgs(1),
	RunE: runRemoteSet,
}

var remoteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured remotes",
	Args:  cobra.NoArgs,
	RunE:  runRemoteList,
}

var remoteShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details for a remote",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteShow,
}

var remoteDefaultCmd = &cobra.Command{
	Use:   "default <name>",
	Short: "Set the default remote (used when -r is given without a value)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteDefault,
}

var remoteRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Delete a remote profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteRemove,
}

var (
	rsHost        string
	rsUser        string
	rsPort        int
	rsKey         string
	rsUseSudo     bool
	rsVitrinaPath string
	rsDomain      string
	rsEmail       string
)

func init() {
	remoteSetCmd.Flags().StringVar(&rsHost, "host", "", "VPS hostname or IP (required)")
	remoteSetCmd.Flags().StringVar(&rsUser, "user", "root", "SSH user")
	remoteSetCmd.Flags().IntVar(&rsPort, "port", 22, "SSH port")
	remoteSetCmd.Flags().StringVar(&rsKey, "key", "", "SSH identity file path")
	remoteSetCmd.Flags().BoolVar(&rsUseSudo, "sudo", false, "Prefix remote commands with sudo")
	remoteSetCmd.Flags().StringVar(&rsVitrinaPath, "vitrina-path", "", "Path to vitrina binary on remote (default: /usr/local/bin/vitrina)")
	remoteSetCmd.Flags().StringVar(&rsDomain, "domain", "", "Base domain for this VPS (used by bootstrap)")
	remoteSetCmd.Flags().StringVar(&rsEmail, "email", "", "Let's Encrypt email for this VPS (used by bootstrap)")
	remoteSetCmd.MarkFlagRequired("host")

	remoteCmd.AddCommand(remoteSetCmd)
	remoteCmd.AddCommand(remoteListCmd)
	remoteCmd.AddCommand(remoteShowCmd)
	remoteCmd.AddCommand(remoteDefaultCmd)
	remoteCmd.AddCommand(remoteRemoveCmd)

	rootCmd.AddCommand(remoteCmd)
}

func runRemoteSet(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	r := &remote.Remote{
		Host:         rsHost,
		User:         rsUser,
		Port:         rsPort,
		IdentityFile: rsKey,
		UseSudo:      rsUseSudo,
		VitrinaPath:  rsVitrinaPath,
		Domain:       rsDomain,
		Email:        rsEmail,
	}
	r.ApplyDefaults()

	cfg.Remotes[name] = r

	if cfg.Default == "" {
		cfg.Default = name
		fmt.Printf("Remote %q saved and set as default.\n", name)
	} else {
		fmt.Printf("Remote %q saved.\n", name)
	}

	return remote.Save(cfg)
}

func runRemoteList(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	if len(cfg.Remotes) == 0 {
		fmt.Println("No remotes configured. Use 'vitrina remote set <name> --host <ip>' to add one.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tUSER:PORT\tDEFAULT")
	for name, r := range cfg.Remotes {
		r.ApplyDefaults()
		def := ""
		if name == cfg.Default {
			def = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s:%d\t%s\n", name, r.Host, r.User, r.Port, def)
	}
	return w.Flush()
}

func runRemoteShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	r, ok := cfg.Remotes[name]
	if !ok {
		return fmt.Errorf("remote %q not found", name)
	}

	r.ApplyDefaults()

	fmt.Printf("Name:        %s\n", name)
	fmt.Printf("Host:        %s\n", r.Host)
	fmt.Printf("User:        %s\n", r.User)
	fmt.Printf("Port:        %d\n", r.Port)
	fmt.Printf("SSH key:     %s\n", r.IdentityFile)
	fmt.Printf("Use sudo:    %v\n", r.UseSudo)
	fmt.Printf("Vitrina:     %s\n", r.VitrinaPath)
	if r.Domain != "" {
		fmt.Printf("Domain:      %s\n", r.Domain)
	}
	if r.Email != "" {
		fmt.Printf("Email:       %s\n", r.Email)
	}
	if name == cfg.Default {
		fmt.Println("Default:     yes")
	}
	return nil
}

func runRemoteDefault(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	if _, ok := cfg.Remotes[name]; !ok {
		return fmt.Errorf("remote %q not found — use 'vitrina remote set %s --host <ip>' first", name, name)
	}

	cfg.Default = name

	if err := remote.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("Default remote set to %q.\n", name)
	return nil
}

func runRemoteRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := remote.Load()
	if err != nil {
		return err
	}

	if _, ok := cfg.Remotes[name]; !ok {
		return fmt.Errorf("remote %q not found", name)
	}

	delete(cfg.Remotes, name)

	if cfg.Default == name {
		cfg.Default = ""
		for n := range cfg.Remotes {
			cfg.Default = n
			break
		}
	}

	if err := remote.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("Remote %q removed.\n", name)
	return nil
}
