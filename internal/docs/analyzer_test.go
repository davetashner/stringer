package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyze_GoRepo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.22\n"), 0o600))

	analysis, err := Analyze(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Base(dir), analysis.Name)
	assert.Equal(t, "Go", analysis.Language)
	require.NotEmpty(t, analysis.TechStack)
	assert.Equal(t, "Go", analysis.TechStack[0].Name)
	assert.Equal(t, "1.22", analysis.TechStack[0].Version)
	assert.NotEmpty(t, analysis.BuildCommands)
}

func TestAnalyze_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	analysis, err := Analyze(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Base(dir), analysis.Name)
	assert.Empty(t, analysis.Language)
	assert.Empty(t, analysis.TechStack)
	assert.Empty(t, analysis.BuildCommands)
	assert.False(t, analysis.HasREADME)
	assert.False(t, analysis.HasAGENTSMD)
}

func TestAnalyze_WithREADME(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello"), 0o600))

	analysis, err := Analyze(dir)
	require.NoError(t, err)

	assert.True(t, analysis.HasREADME)
	assert.False(t, analysis.HasAGENTSMD)
}

func TestAnalyze_WithAGENTSMD(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0o600))

	analysis, err := Analyze(dir)
	require.NoError(t, err)

	assert.True(t, analysis.HasAGENTSMD)
}

func TestBuildDirectoryTree(t *testing.T) {
	dir := t.TempDir()

	// Create nested directory structure.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "app"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "pkg"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "app", "main.go"), []byte("package main"), 0o600))

	entries := buildDirectoryTree(dir, 3)

	// Should contain cmd, internal, go.mod at minimum.
	paths := make(map[string]bool)
	for _, e := range entries {
		paths[e.Path] = true
	}

	assert.True(t, paths["cmd"], "should contain cmd/")
	assert.True(t, paths["internal"], "should contain internal/")
	assert.True(t, paths["go.mod"], "should contain go.mod")
	assert.True(t, paths[filepath.Join("cmd", "app")], "should contain cmd/app/")
	assert.True(t, paths[filepath.Join("cmd", "app", "main.go")], "should contain cmd/app/main.go")
}

func TestBuildDirectoryTree_DepthLimit(t *testing.T) {
	dir := t.TempDir()

	// Create a deeply nested structure.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d", "e"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "c", "d", "e", "deep.txt"), []byte("x"), 0o600))

	entries := buildDirectoryTree(dir, 3)

	// Should not include depth > 3.
	for _, e := range entries {
		assert.LessOrEqual(t, e.Depth, 3, "entry %s exceeds max depth", e.Path)
	}
}

func TestBuildDirectoryTree_SkipHidden(t *testing.T) {
	dir := t.TempDir()

	// Create .git and other hidden directories.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules", "foo"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o600))

	entries := buildDirectoryTree(dir, 3)

	for _, e := range entries {
		assert.NotEqual(t, ".git", filepath.Base(e.Path), "should skip .git")
		assert.NotEqual(t, "node_modules", filepath.Base(e.Path), "should skip node_modules")
		assert.NotContains(t, e.Path, ".git", "should not contain .git paths")
		assert.NotContains(t, e.Path, "node_modules", "should not contain node_modules paths")
	}

	// Should contain src.
	paths := make(map[string]bool)
	for _, e := range entries {
		paths[e.Path] = true
	}
	assert.True(t, paths["src"], "should contain src/")
}

func TestAnalyze_DetectsPatterns(t *testing.T) {
	dir := t.TempDir()

	// Create internal dir and go.mod with cobra.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs", "decisions"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o750))
	goMod := "module test\n\ngo 1.22\n\nrequire (\n\tgithub.com/spf13/cobra v1.8.0\n)\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600))

	analysis, err := Analyze(dir)
	require.NoError(t, err)

	patternNames := make(map[string]bool)
	for _, p := range analysis.Patterns {
		patternNames[p.Name] = true
	}

	assert.True(t, patternNames["Cobra CLI"], "should detect Cobra CLI pattern")
	assert.True(t, patternNames["Go Internal Packages"], "should detect internal packages pattern")
	assert.True(t, patternNames["Decision Records"], "should detect decision records pattern")
	assert.True(t, patternNames["GitHub Actions CI"], "should detect GitHub Actions pattern")
}

func TestBuildDirectoryTree_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	entries := buildDirectoryTree(dir, 3)
	assert.Empty(t, entries)
}

func TestBuildDirectoryTree_SkipVendor(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor", "lib"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))

	entries := buildDirectoryTree(dir, 3)

	for _, e := range entries {
		assert.NotEqual(t, "vendor", filepath.Base(e.Path), "should skip vendor/")
	}
}

func TestDirEntry_Fields(t *testing.T) {
	e := DirEntry{Path: "cmd/app", IsDir: true, Depth: 2}
	assert.Equal(t, "cmd/app", e.Path)
	assert.True(t, e.IsDir)
	assert.Equal(t, 2, e.Depth)
}
