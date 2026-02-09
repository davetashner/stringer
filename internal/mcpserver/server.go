package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New creates a new MCP server with stringer's tools registered.
func New(version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "stringer",
		Title:   "Stringer — Codebase Archaeology",
		Version: version,
	}, nil)

	registerTools(server)
	return server
}

// Run creates an MCP server and runs it on stdio transport.
// It blocks until the client disconnects or the context is cancelled.
func Run(ctx context.Context, version string) error {
	server := New(version)
	return server.Run(ctx, &mcp.StdioTransport{})
}
