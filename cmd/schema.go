package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

type manifest struct {
	Version  string        `json:"version"`
	Commands []commandSpec `json:"commands"`
	Errors   []string      `json:"errors"`
}

type commandSpec struct {
	Command      string              `json:"command"`
	Args         []argSpec           `json:"args,omitempty"`
	Flags        map[string]flagSpec `json:"flags,omitempty"`
	RequiresRoot bool                `json:"requires_root"`
	Mutates      []string            `json:"mutates,omitempty"`
	OutputSchema string              `json:"output_schema"`
	Destructive  bool                `json:"destructive,omitempty"`
}

type argSpec struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}
type flagSpec struct {
	Type        string `json:"type"`
	Default     any    `json:"default,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
}

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the machine-readable CLI manifest",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaFormat != "json" {
			return fmt.Errorf("unsupported schema format %q; only json is supported", schemaFormat)
		}
		value := manifest{Version: "1", Errors: []string{
			"ERR_VALIDATION", "ERR_NOT_FOUND", "ERR_STATE_CONFLICT", "ERR_TIMEOUT",
			"ERR_INTERACTION_REQUIRED", "ERR_SSH_TIMEOUT", "ERR_SSH_AUTHENTICATION",
			"ERR_SSH_UNAVAILABLE", "ERR_REMOTE_COMMAND_FAILED", "ERR_DOCKER_FAILED",
			"ERR_GIT_FAILED", "ERR_CADDY_FAILED", "ERR_PARTIAL_SUCCESS",
		}}
		rootCmd.Commands()
		value.Commands = []commandSpec{
			{Command: "add", Args: []argSpec{{Name: "subdomain", Required: true}}, RequiresRoot: true, Mutates: []string{"registry", "caddy", "filesystem"}, OutputSchema: "OperationResult"},
			{Command: "deploy", Args: []argSpec{{Name: "subdomain", Required: true}, {Name: "git_url", Required: true}}, RequiresRoot: true, Mutates: []string{"registry", "caddy", "docker", "filesystem"}, OutputSchema: "OperationResult"},
			{Command: "remove", Args: []argSpec{{Name: "subdomain", Required: true}}, Flags: map[string]flagSpec{"clean": {Type: "boolean", Destructive: true}, "dry_run": {Type: "boolean"}, "yes": {Type: "boolean"}}, RequiresRoot: true, Mutates: []string{"registry", "caddy", "docker", "filesystem"}, OutputSchema: "OperationResult", Destructive: true},
			{Command: "redeploy", Args: []argSpec{{Name: "subdomain", Required: true}}, RequiresRoot: true, Mutates: []string{"git", "docker", "caddy"}, OutputSchema: "OperationResult"},
			{Command: "env set", Args: []argSpec{{Name: "subdomain", Required: true}}, RequiresRoot: true, Mutates: []string{"filesystem", "registry", "docker"}, OutputSchema: "OperationResult"},
		}
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var schemaFormat string

func init() {
	schemaCmd.Flags().StringVar(&schemaFormat, "format", "json", "Manifest format (json)")
	rootCmd.AddCommand(schemaCmd)
}
