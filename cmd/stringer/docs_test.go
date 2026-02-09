package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetDocsFlags resets all package-level docs flags to their default values.
// It also resets cobra's internal help flag to prevent state leaking between tests.
func resetDocsFlags() {
	docsOutput = ""
	docsUpdate = false
	// Reset the help flag on the docsCmd, which cobra sets when --help is used.
	if h := docsCmd.Flags().Lookup("help"); h != nil {
		_ = h.Value.Set("false")
	}
}

func TestDocsCmd_Stdout(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()

	// Create a minimal Go repo.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd"), 0o750))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "# AGENTS.md")
	assert.Contains(t, out, "## Architecture")
	assert.Contains(t, out, "## Tech Stack")
	assert.Contains(t, out, "**Go** 1.22")
	assert.Contains(t, out, "## Build & Test")
}

func TestDocsCmd_OutputFile(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "AGENTS.md")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "-o", outFile, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# AGENTS.md")
	assert.Contains(t, content, "**Go** 1.22")
}

func TestDocsCmd_InvalidPath(t *testing.T) {
	resetDocsFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", "/nonexistent/path/that/does/not/exist"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestDocsCmd_PathIsFile(t *testing.T) {
	resetDocsFlags()
	tmp := filepath.Join(t.TempDir(), "somefile.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", tmp})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestDocsCmd_Update(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()

	// Create go.mod.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.24\n"), 0o600))

	// Create existing AGENTS.md with markers and manual content.
	existing := `# AGENTS.md — myproject

My custom intro

<!-- stringer:auto:start:architecture -->
## Architecture

Old tree
<!-- stringer:auto:end:architecture -->

## Custom Section

My manual content

<!-- stringer:auto:start:techstack -->
## Tech Stack

- **OldLang**
<!-- stringer:auto:end:techstack -->

<!-- stringer:auto:start:build -->
## Build & Test

old commands
<!-- stringer:auto:end:build -->
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "--update", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "My custom intro")
	assert.Contains(t, out, "## Custom Section")
	assert.Contains(t, out, "My manual content")
	assert.Contains(t, out, "**Go** 1.24")
	assert.NotContains(t, out, "OldLang")
}

func TestDocsCmd_UpdateNoExistingFile(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir() // no AGENTS.md

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "--update"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no existing AGENTS.md")
}

func TestDocsCmd_UpdateWithOutputFile(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "updated-AGENTS.md")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	existing := `# AGENTS.md

<!-- stringer:auto:start:techstack -->
## Tech Stack
- Old
<!-- stringer:auto:end:techstack -->
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "--update", "-o", outFile, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "**Go** 1.22")
	assert.NotContains(t, content, "- Old")
}

func TestDocsCmd_DefaultPath(t *testing.T) {
	resetDocsFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", "--quiet"})

	// Uses current directory, which is cmd/stringer — should work.
	err := cmd.Execute()
	require.NoError(t, err)

	assert.NotEmpty(t, stdout.String())
}

func TestDocsCmd_FlagsRegistered(t *testing.T) {
	flags := []struct {
		name      string
		shorthand string
	}{
		{"output", "o"},
		{"update", ""},
	}

	for _, ff := range flags {
		t.Run(ff.name, func(t *testing.T) {
			f := docsCmd.Flags().Lookup(ff.name)
			require.NotNil(t, f, "flag --%s not registered", ff.name)
			if ff.shorthand != "" {
				s := docsCmd.Flags().ShorthandLookup(ff.shorthand)
				require.NotNil(t, s, "shorthand -%s not registered", ff.shorthand)
				assert.Equal(t, ff.name, s.Name)
			}
		})
	}
}

func TestDocsCmd_Help(t *testing.T) {
	resetDocsFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	// The help output shows the Long description, which starts with "Analyze".
	assert.Contains(t, out, "Analyze a repository")
	assert.Contains(t, out, "AGENTS.md")
	assert.Contains(t, out, "--output")
	assert.Contains(t, out, "--update")
}

func TestDocsCmd_OutputToInvalidFile(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "-o", "/nonexistent/dir/file.md", "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

func TestDocsCmd_EmptyDir(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "# AGENTS.md")
}

func TestDocsCmd_SymlinkPath(t *testing.T) {
	resetDocsFlags()
	target := t.TempDir()
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "link")
	require.NoError(t, os.Symlink(target, link))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", link, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.NotEmpty(t, stdout.String())
}

func TestDocsCmd_UpdateWithInvalidOutputFile(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	existing := `# AGENTS.md

<!-- stringer:auto:start:techstack -->
## Tech Stack
- Old
<!-- stringer:auto:end:techstack -->
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "--update", "-o", "/nonexistent/dir/agents.md", "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

func TestDocsCmd_InRootHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.True(t, strings.Contains(out, "docs"), "root help should list docs subcommand")
}
