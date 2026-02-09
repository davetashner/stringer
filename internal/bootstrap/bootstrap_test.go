package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/config"
)

func TestRun_GoRepoWithGitHub(t *testing.T) {
	dir := initGitRepo(t, "https://github.com/octocat/hello-world.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	result, err := Run(InitConfig{RepoPath: dir})
	require.NoError(t, err)

	assert.Equal(t, "Go", result.Language)
	assert.True(t, result.HasGitHub)
	assert.Len(t, result.Actions, 2)

	// Config created.
	assert.Equal(t, config.FileName, result.Actions[0].File)
	assert.Equal(t, "created", result.Actions[0].Operation)

	// AGENTS.md created.
	assert.Equal(t, "AGENTS.md", result.Actions[1].File)
	assert.Equal(t, "created", result.Actions[1].Operation)

	// Verify files exist.
	assert.FileExists(t, filepath.Join(dir, config.FileName))
	assert.FileExists(t, filepath.Join(dir, "AGENTS.md"))
}

func TestRun_NonGitRepo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	result, err := Run(InitConfig{RepoPath: dir})
	require.NoError(t, err)

	assert.False(t, result.HasGitHub)
	assert.Len(t, result.Actions, 2)
}

func TestRun_Idempotent(t *testing.T) {
	dir := initGitRepo(t, "https://github.com/octocat/hello-world.git")

	// First run.
	result1, err := Run(InitConfig{RepoPath: dir})
	require.NoError(t, err)
	assert.Equal(t, "created", result1.Actions[0].Operation)
	assert.Equal(t, "created", result1.Actions[1].Operation)

	// Second run — both should be skipped.
	result2, err := Run(InitConfig{RepoPath: dir})
	require.NoError(t, err)
	assert.Equal(t, "skipped", result2.Actions[0].Operation)
	assert.Equal(t, "skipped", result2.Actions[1].Operation)
}

func TestRun_ForceRegeneratesConfig(t *testing.T) {
	dir := initGitRepo(t, "https://github.com/octocat/hello-world.git")

	// First run.
	_, err := Run(InitConfig{RepoPath: dir})
	require.NoError(t, err)

	// Second run with force.
	result, err := Run(InitConfig{RepoPath: dir, Force: true})
	require.NoError(t, err)

	// Config regenerated, AGENTS.md skipped (force only affects config).
	assert.Equal(t, "created", result.Actions[0].Operation)
	assert.Contains(t, result.Actions[0].Description, "regenerated")
	assert.Equal(t, "skipped", result.Actions[1].Operation)
}

func TestRun_ExistingAGENTSMD(t *testing.T) {
	dir := t.TempDir()
	existing := "# My Project AGENTS.md\n\nCustom content.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	result, err := Run(InitConfig{RepoPath: dir})
	require.NoError(t, err)

	// AGENTS.md updated (appended), not created.
	assert.Equal(t, "updated", result.Actions[1].Operation)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Contains(t, string(data), "Custom content.")
	assert.Contains(t, string(data), "## Stringer Integration")
}
