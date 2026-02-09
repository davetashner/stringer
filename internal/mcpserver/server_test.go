package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_ReturnsServer(t *testing.T) {
	server := New("v1.0.0-test")
	assert.NotNil(t, server)
}

func TestRun_WithInMemoryTransport(t *testing.T) {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, "v1.0.0-test", serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v1.0.0",
	}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer session.Close() //nolint:errcheck // best-effort close in test

	result, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, result.Tools, 4)

	cancel()
}

func TestServer_ListsTools(t *testing.T) {
	server := New("v1.0.0-test")

	// Create in-memory transport pair.
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run server in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, serverTransport)
	}()

	// Connect client.
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v1.0.0",
	}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer session.Close() //nolint:errcheck // best-effort close in test

	// List tools.
	result, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	// Should have 4 tools.
	assert.Len(t, result.Tools, 4)

	// Verify tool names.
	names := make(map[string]bool)
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	assert.True(t, names["scan"], "should have scan tool")
	assert.True(t, names["report"], "should have report tool")
	assert.True(t, names["context"], "should have context tool")
	assert.True(t, names["docs"], "should have docs tool")

	cancel()
}
