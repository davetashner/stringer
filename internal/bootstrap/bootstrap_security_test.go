package bootstrap

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/davetashner/stringer/internal/config"
)

// Security tests for the bootstrap/init subsystem (DX1.7).

func TestGenerateConfig_SecurityFilePermissions(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateConfig(dir, true, false)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, config.FileName))
	require.NoError(t, err)

	perm := info.Mode().Perm()
	assert.Equal(t, fs.FileMode(0o644), perm, "config should be 0644")
	assert.Zero(t, perm&0o002, "config must not be world-writable")
}

func TestAppendAgentSnippet_SecurityFilePermissions(t *testing.T) {
	dir := t.TempDir()

	_, err := AppendAgentSnippet(dir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)

	perm := info.Mode().Perm()
	assert.Equal(t, fs.FileMode(0o644), perm, "AGENTS.md should be 0644")
	assert.Zero(t, perm&0o002, "AGENTS.md must not be world-writable")
}

func TestGenerateConfig_SecurityOutputWithinRepoPath(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(dir)

	_, err := GenerateConfig(dir, true, false)
	require.NoError(t, err)

	// The config file must exist inside dir, not in parent.
	assert.FileExists(t, filepath.Join(dir, config.FileName))

	// No stringer config should appear in the parent directory.
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	for _, e := range entries {
		if e.Name() == config.FileName {
			// Only valid if it's inside our dir.
			assert.Equal(t, filepath.Base(dir), e.Name(),
				"config file leaked to parent directory")
		}
	}
}

func TestGenerateConfig_SecurityNoTemplateLeakage(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateConfig(dir, true, false)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(data)

	// No unresolved Go template directives.
	assert.NotContains(t, content, "{{", "output must not contain raw template syntax")
	assert.NotContains(t, content, "}}", "output must not contain raw template syntax")

	// Must be valid YAML.
	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg), "output must be valid YAML")
}

func TestAppendAgentSnippet_SecurityPreservesContentVerbatim(t *testing.T) {
	dir := t.TempDir()

	// Write content that looks like template syntax and shell commands.
	dangerous := "# Config\n\nRun `rm -rf /` for fun.\n\nValue: {{ .Secret }}\n\n$HOME/bin\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(dangerous), 0o600))

	_, err := AppendAgentSnippet(dir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(data)

	// Original dangerous content must be preserved verbatim (not executed/interpreted).
	assert.Contains(t, content, "rm -rf /")
	assert.Contains(t, content, "{{ .Secret }}")
	assert.Contains(t, content, "$HOME/bin")
}

func TestRun_SecurityIdempotencyStress(t *testing.T) {
	dir := t.TempDir()

	// Run init 5 times.
	for i := 0; i < 5; i++ {
		_, err := Run(InitConfig{RepoPath: dir})
		require.NoError(t, err, "run %d failed", i+1)
	}

	// Config file should exist.
	configData, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(configData, &cfg), "config must be valid YAML after 5 runs")

	// AGENTS.md should have exactly 1 marker pair.
	agentsData, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(agentsData)

	startCount := strings.Count(content, markerStart)
	endCount := strings.Count(content, markerEnd)
	assert.Equal(t, 1, startCount, "should have exactly 1 start marker after 5 runs")
	assert.Equal(t, 1, endCount, "should have exactly 1 end marker after 5 runs")
}

func TestGenerateConfig_SecurityRepoPathIsFile_Fails(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0o600))

	// Attempting to write config into a file path should fail.
	_, err := GenerateConfig(filePath, true, false)
	require.Error(t, err, "writing config to a file-as-directory should fail")
}

func TestRun_SecuritySymlinkToValidDir(t *testing.T) {
	// Create real target directory.
	realDir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)

	// Create a symlink to the real directory.
	linkParent := t.TempDir()
	linkPath := filepath.Join(linkParent, "linked-repo")
	require.NoError(t, os.Symlink(realDir, linkPath))

	result, err := Run(InitConfig{RepoPath: linkPath})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Files must be created in the real directory, not the symlink parent.
	assert.FileExists(t, filepath.Join(realDir, config.FileName))
	assert.FileExists(t, filepath.Join(realDir, "AGENTS.md"))

	// Symlink parent should not contain stringer files.
	assert.NoFileExists(t, filepath.Join(linkParent, config.FileName))
	assert.NoFileExists(t, filepath.Join(linkParent, "AGENTS.md"))
}
