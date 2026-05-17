package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	vitrinamcp "vitrina/internal/mcp"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start an MCP server for AI agent integration",
	Long: `Start a Model Context Protocol (MCP) server that exposes
Vitrina commands as tools for AI agents. Communicates via stdio
using JSON-RPC, compatible with Claude, Cursor, opencode, and
other MCP clients.

Add to your MCP client configuration:
  {
    "mcpServers": {
      "vitrina": {
        "command": "sudo",
        "args": ["vitrina", "mcp"]
      }
    }
  }`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	s := vitrinamcp.NewServer()

	stdio := server.NewStdioServer(s)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}