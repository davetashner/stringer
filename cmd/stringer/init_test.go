package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/config"
)

// resetInitFlags resets all package-level init flags to their default values.
func resetInitFlags() {
	initForce = false
	if h := initCmd.Flags().Lookup("help"); h != nil {
		_ = h.Value.Set("false")
	}
}

func TestInitCmd_CreatesFiles(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Config file created.
	assert.FileExists(t, filepath.Join(dir, config.FileName))

	// AGENTS.md created.
	assert.FileExists(t, filepath.Join(dir, "AGENTS.md"))

	out := stdout.String()
	assert.Contains(t, out, "stringer init complete")
}

func TestInitCmd_Idempotent(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()

	// First run.
	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})
	require.NoError(t, cmd.Execute())

	// Second run — should skip both files.
	resetInitFlags()
	cmd2, stdout2, _ := newTestCmd()
	cmd2.SetArgs([]string{"init", dir, "--quiet"})
	require.NoError(t, cmd2.Execute())

	out := stdout2.String()
	assert.Contains(t, out, "stringer init complete")
	// Should show already-exists status (config and AGENTS.md both skipped).
	assert.Contains(t, out, "already exists")
	assert.Contains(t, out, "already present")
}

func TestInitCmd_Force(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()

	// First run.
	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})
	require.NoError(t, cmd.Execute())

	// Second run with --force.
	resetInitFlags()
	cmd2, stdout2, _ := newTestCmd()
	cmd2.SetArgs([]string{"init", dir, "--force", "--quiet"})
	require.NoError(t, cmd2.Execute())

	out := stdout2.String()
	assert.Contains(t, out, "regenerated")
}

func TestInitCmd_InvalidPath(t *testing.T) {
	resetInitFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", "/nonexistent/path/that/does/not/exist"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestInitCmd_PathIsFile(t *testing.T) {
	resetInitFlags()
	tmp := filepath.Join(t.TempDir(), "somefile.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", tmp})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestInitCmd_DefaultPath(t *testing.T) {
	resetInitFlags()

	// Save current directory and create a temp dir to work in.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"init", "--quiet"})

	err = cmd.Execute()
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, config.FileName))
	assert.Contains(t, stdout.String(), "stringer init complete")
}

func TestInitCmd_FlagsRegistered(t *testing.T) {
	f := initCmd.Flags().Lookup("force")
	require.NotNil(t, f, "flag --force not registered")
	assert.Equal(t, "false", f.DefValue)
}

func TestInitCmd_Help(t *testing.T) {
	resetInitFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"init", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Initialize stringer")
	assert.Contains(t, out, "--force")
}

func TestInitCmd_InRootHelp(t *testing.T) {
	resetInitFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.True(t, strings.Contains(out, "init"), "root help should list init subcommand")
}

func TestInitCmd_WithGitHubRemote(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()

	// Set up git repo with GitHub remote.
	gitInit(t, dir, "https://github.com/octocat/hello-world.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Config should mention GitHub enabled.
	out := stdout.String()
	assert.Contains(t, out, "github collector enabled")

	// Verify config file has github enabled.
	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: true")
}

func TestInitCmd_PreservesExistingAGENTSMD(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()
	existing := "# Custom AGENTS.md\n\nMy content.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "My content.")
	assert.Contains(t, content, "## Stringer Integration")
}

// gitInit creates a git repo with a remote in the given directory.
func gitInit(t *testing.T, dir, remoteURL string) {
	t.Helper()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "remote", "add", "origin", remoteURL)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o600))
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", "init")
}

// runGitCmd runs a git command in the given directory.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}
