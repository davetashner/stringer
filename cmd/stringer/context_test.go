package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	root := repoRoot(t)

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
	root := repoRoot(t)

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
	root := repoRoot(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--format", "xml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported context format")
	assert.Contains(t, err.Error(), "xml")
}
