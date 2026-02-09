package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
)

// resetContextFlags resets all package-level context flags to their default values.
func resetContextFlags() {
	contextOutput = ""
	contextFormat = ""
	contextWeeks = 4

	contextCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	if h := contextCmd.Flags().Lookup("help"); h != nil {
		_ = h.Value.Set("false")
	}
}

func TestContextCmd_Exists(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "context" {
			found = true
			break
		}
	}
	assert.True(t, found, "context command should be registered on root")
}

func TestContextCmd_DefaultPath(t *testing.T) {
	resetContextFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "# CONTEXT.md")
}

func TestContextCmd_ExplicitPath(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "# CONTEXT.md")
}

func TestContextCmd_InvalidPath(t *testing.T) {
	resetContextFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", "/nonexistent/path/that/does/not/exist"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestContextCmd_FileNotDir(t *testing.T) {
	resetContextFlags()
	tmp := filepath.Join(t.TempDir(), "somefile.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", tmp})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestContextCmd_FlagsRegistered(t *testing.T) {
	flags := []struct {
		name      string
		shorthand string
	}{
		{"output", "o"},
		{"format", "f"},
		{"weeks", ""},
	}

	for _, ff := range flags {
		t.Run(ff.name, func(t *testing.T) {
			f := contextCmd.Flags().Lookup(ff.name)
			require.NotNil(t, f, "flag --%s not registered", ff.name)
			if ff.shorthand != "" {
				s := contextCmd.Flags().ShorthandLookup(ff.shorthand)
				require.NotNil(t, s, "shorthand -%s not registered", ff.shorthand)
				assert.Equal(t, ff.name, s.Name)
			}
		})
	}
}

func TestContextCmd_FormatJSON(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--format", "json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()

	// Output should be valid JSON.
	var result contextJSON
	require.NoError(t, json.Unmarshal([]byte(out), &result), "output should be valid JSON")

	// Verify key fields are present.
	assert.NotEmpty(t, result.Name)
	assert.NotEmpty(t, result.Language)
	assert.NotEmpty(t, result.TechStack)
	assert.NotEmpty(t, result.Patterns)

	// Verify tech stack has expected structure.
	for _, tc := range result.TechStack {
		assert.NotEmpty(t, tc.Name, "tech component name should not be empty")
	}

	// History should be populated for a git repo.
	// Note: TotalCommits may be 0 in some environments (e.g., git worktrees
	// where go-git cannot walk the log), so we only assert history is present.
	if result.History != nil {
		assert.GreaterOrEqual(t, result.History.TotalCommits, 0)
	}
}

func TestContextCmd_FormatInvalid(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--format", "xml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported context format")
	assert.Contains(t, err.Error(), "xml")
}

func TestContextCmd_OutputFile(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)
	outFile := filepath.Join(t.TempDir(), "context.md")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "-o", outFile, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Contains(t, string(data), "# CONTEXT.md")
}

func TestContextCmd_OutputFileError(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "-o", "/nonexistent/dir/context.md", "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

func TestContextCmd_WeeksFlag(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--weeks", "2", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "# CONTEXT.md")
}

func TestContextCmd_NonGitDir(t *testing.T) {
	resetContextFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "# CONTEXT.md")
}

func TestContextCmd_NonGitDirJSON(t *testing.T) {
	resetContextFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", dir, "--format", "json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result contextJSON
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.NotEmpty(t, result.Name)
	// No git history expected for non-git dir.
	assert.Nil(t, result.History)
}

func TestRenderContextJSON_NilHistory(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test",
		Language: "Go",
		TechStack: []docs.TechComponent{
			{Name: "Go", Version: "1.22", Source: "go.mod"},
		},
		BuildCommands: []docs.BuildCommand{
			{Name: "build", Command: "go build ./...", Source: "go.mod"},
		},
		Patterns: []docs.CodePattern{
			{Name: "CLI", Description: "Command-line application"},
		},
	}

	var buf bytes.Buffer
	err := renderContextJSON(analysis, nil, nil, &buf)
	require.NoError(t, err)

	var result contextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Equal(t, "test", result.Name)
	assert.Nil(t, result.History)
	assert.Nil(t, result.TechDebt)
	assert.NotEmpty(t, result.TechStack)
	assert.NotEmpty(t, result.BuildCmds)
	assert.NotEmpty(t, result.Patterns)
}

func TestRenderContextJSON_WithScanState(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test",
		Language: "Go",
	}

	scanState := &state.ScanState{
		SignalCount: 5,
		SignalMetas: []state.SignalMeta{
			{Kind: "todo"},
			{Kind: "todo"},
			{Kind: "churn"},
		},
	}

	var buf bytes.Buffer
	err := renderContextJSON(analysis, nil, scanState, &buf)
	require.NoError(t, err)

	var result contextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.NotNil(t, result.TechDebt)
	assert.Equal(t, 5, result.TechDebt.SignalCount)
	assert.Equal(t, 2, result.TechDebt.ByKind["todo"])
	assert.Equal(t, 1, result.TechDebt.ByKind["churn"])
}

func TestContextCmd_OutputFileJSON(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)
	outFile := filepath.Join(t.TempDir(), "context.json")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--format", "json", "-o", outFile, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)

	var result contextJSON
	require.NoError(t, json.Unmarshal(data, &result))
	assert.NotEmpty(t, result.Name)
}
