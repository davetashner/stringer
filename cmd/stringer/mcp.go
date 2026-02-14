// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/mcpserver"
)

// mcpCmd is the parent command for MCP-related subcommands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol server commands",
	Long:  "Commands for running stringer as an MCP server, exposing scan, report, context, and docs tools to AI agents.",
}

// mcpServeCmd runs the MCP server over stdio.
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the MCP server over stdio",
	Long: `Start an MCP server on stdin/stdout, exposing stringer's core tools:
  - scan:    Scan a repository for actionable work items
  - report:  Generate a repository health report
  - context: Generate a CONTEXT.md for agent onboarding
  - docs:    Generate or update an AGENTS.md scaffold

The server communicates using the Model Context Protocol (MCP) over stdio
transport, enabling AI agents to call stringer tools directly.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return mcpserver.Run(cmd.Context(), Version, &mcp.StdioTransport{})
	},
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)
}
