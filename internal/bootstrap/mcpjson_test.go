package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/testable"
)

func TestGenerateMCPConfig_CreatesWhenClaudeExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))

	action, err := GenerateMCPConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, ".mcp.json", action.File)
	assert.Equal(t, "created", action.Operation)

	// Verify file contents.
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test path
	require.NoError(t, err)

	var cfg mcpConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	assert.Contains(t, cfg.MCPServers, "stringer")

	var entry mcpServerEntry
	require.NoError(t, json.Unmarshal(cfg.MCPServers["stringer"], &entry))
	assert.Equal(t, "stringer", entry.Command)
	assert.Equal(t, []string{"mcp", "serve"}, entry.Args)
}

func TestGenerateMCPConfig_SkipsWithoutClaude(t *testing.T) {
	dir := t.TempDir()

	action, err := GenerateMCPConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, ".mcp.json", action.File)
	assert.Equal(t, "skipped", action.Operation)
	assert.Contains(t, action.Description, "Claude Code not detected")

	// .mcp.json should not exist.
	_, err = os.Stat(filepath.Join(dir, ".mcp.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestGenerateMCPConfig_MergesIntoExisting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))

	// Create existing .mcp.json with another server.
	existing := `{
  "mcpServers": {
    "other-tool": {"command": "other", "args": ["serve"]}
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(existing), 0o600))

	action, err := GenerateMCPConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, "updated", action.Operation)

	// Verify both entries exist.
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test path
	require.NoError(t, err)

	var cfg mcpConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	assert.Contains(t, cfg.MCPServers, "stringer")
	assert.Contains(t, cfg.MCPServers, "other-tool")
}

func TestGenerateMCPConfig_SkipsWhenAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))

	// Create .mcp.json with stringer already configured.
	existing := `{
  "mcpServers": {
    "stringer": {"command": "stringer", "args": ["mcp", "serve"]}
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(existing), 0o600))

	action, err := GenerateMCPConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, "skipped", action.Operation)
	assert.Contains(t, action.Description, "already configured")
}

func TestGenerateMCPConfig_Idempotent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))

	// First run — creates.
	action1, err := GenerateMCPConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, "created", action1.Operation)

	// Second run — skips.
	action2, err := GenerateMCPConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, "skipped", action2.Operation)
}

func TestGenerateMCPConfig_WriteFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))

	FS = &testable.MockFileSystem{
		ReadFileFn: func(_ string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			return os.ErrPermission
		},
	}

	_, err := GenerateMCPConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating .mcp.json")
}

func TestGenerateMCPConfig_InvalidExistingJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("not json"), 0o600))

	_, err := GenerateMCPConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing .mcp.json")
}
