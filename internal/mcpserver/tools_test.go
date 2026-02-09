package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initTestRepo creates a small git repo for testing.
func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	var err error
	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	// go.mod so project detection works.
	writeTestFile(t, dir, "go.mod", "module testrepo\n\ngo 1.22\n")
	writeTestFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	// TODO: Add proper CLI argument parsing
	fmt.Println("hello world")
}
`)

	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.name=Alice", "-c", "user.email=alice@test.com",
		"commit", "-m", "Initial commit")

	return dir
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	parent := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(parent, 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestHandleScan_JSONOutput(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
		Format:     "json",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "TODO")
	// Should be valid JSON.
	assert.True(t, json.Valid([]byte(text)), "output should be valid JSON")
}

func TestHandleScan_DefaultsToJSON(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)), "default output should be valid JSON")
}

func TestHandleScan_MarkdownFormat(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
		Format:     "markdown",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "TODO")
}

func TestHandleScan_InvalidFormat(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:   dir,
		Format: "invalid",
	}

	_, _, err := handleScan(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestHandleScan_InvalidPath(t *testing.T) {
	input := ScanInput{
		Path: "/nonexistent/path",
	}

	_, _, err := handleScan(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestHandleScan_KindFilter(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
		Kind:       "fixme",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	// If there are no FIXMEs in the test repo, should get empty but valid output.
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleScan_MinConfidence(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:          dir,
		Collectors:    "todos",
		MinConfidence: 0.99,
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleReport_JSONOutput(t *testing.T) {
	dir := initTestRepo(t)

	input := ReportInput{
		Path:       dir,
		Collectors: "todos",
	}

	result, _, err := handleReport(context.Background(), nil, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)), "report output should be valid JSON")
	assert.Contains(t, text, "repository")
	assert.Contains(t, text, "signals")
}

func TestHandleReport_InvalidPath(t *testing.T) {
	input := ReportInput{
		Path: "/nonexistent/path",
	}

	_, _, err := handleReport(context.Background(), nil, input)
	assert.Error(t, err)
}

func TestHandleContext_JSONOutput(t *testing.T) {
	dir := initTestRepo(t)

	input := ContextInput{
		Path: dir,
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)), "context output should be valid JSON")
	assert.Contains(t, text, "Go") // should detect Go as language
}

func TestHandleContext_MarkdownOutput(t *testing.T) {
	dir := initTestRepo(t)

	input := ContextInput{
		Path:   dir,
		Format: "markdown",
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "CONTEXT.md")
}

func TestHandleContext_InvalidFormat(t *testing.T) {
	dir := initTestRepo(t)

	input := ContextInput{
		Path:   dir,
		Format: "invalid",
	}

	_, _, err := handleContext(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestHandleDocs_Generate(t *testing.T) {
	dir := initTestRepo(t)

	input := DocsInput{
		Path: dir,
	}

	result, _, err := handleDocs(context.Background(), nil, input)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "AGENTS.md")
}

func TestHandleDocs_InvalidPath(t *testing.T) {
	input := DocsInput{
		Path: "/nonexistent/path",
	}

	_, _, err := handleDocs(context.Background(), nil, input)
	assert.Error(t, err)
}

func TestHandleScan_InvalidCollector(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "nonexistent_collector",
	}

	_, _, err := handleScan(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "available:")
}

func TestHandleScan_GitDepthAndSince(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
		GitDepth:   50,
		GitSince:   "30d",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleScan_SubdirectoryScan(t *testing.T) {
	dir := initTestRepo(t)

	// Create a subdirectory with a TODO.
	subdir := filepath.Join(dir, "pkg", "sub")
	require.NoError(t, os.MkdirAll(subdir, 0o750))
	writeTestFile(t, dir, "pkg/sub/file.go", `package sub
// TODO: fix this
`)
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-m", "add subdir")

	input := ScanInput{
		Path:       subdir,
		Collectors: "todos",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
	assert.Contains(t, text, "TODO")
}

func TestHandleScan_BeadsAwareDedup(t *testing.T) {
	dir := initTestRepo(t)

	// Create a .beads directory with an existing issue matching our TODO.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".beads"), 0o750))
	writeTestFile(t, dir, ".beads/issues.jsonl", `{"id":"test-001","title":"Add proper CLI argument parsing","status":"open"}
`)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleScan_BeadsLoadError(t *testing.T) {
	dir := initTestRepo(t)

	// Create a corrupted .beads/issues.jsonl to trigger LoadBeads error (slog.Warn path).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".beads"), 0o750))
	writeTestFile(t, dir, ".beads/issues.jsonl", "not valid json\n")

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
	}

	// Should succeed — beads load error is a warning, not fatal.
	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
	assert.Contains(t, text, "TODO")
}

func TestHandleScan_ConfidenceFilterKeepsHighConfidence(t *testing.T) {
	dir := initTestRepo(t)

	// Run with low threshold — should keep the TODO signal.
	input := ScanInput{
		Path:          dir,
		Collectors:    "todos",
		MinConfidence: 0.1,
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "TODO")
}

func TestHandleScan_KindFilterMatchesTodo(t *testing.T) {
	dir := initTestRepo(t)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
		Kind:       "todo",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "TODO")
}

func TestHandleScan_ConfigLoadError(t *testing.T) {
	dir := initTestRepo(t)

	// Write an invalid .stringer.yaml to trigger config load error.
	writeTestFile(t, dir, ".stringer.yaml", "invalid: [yaml: {broken")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-m", "add broken config")

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
	}

	_, _, err := handleScan(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestHandleReport_GitDepthAndSince(t *testing.T) {
	dir := initTestRepo(t)

	input := ReportInput{
		Path:       dir,
		Collectors: "todos",
		GitDepth:   50,
		GitSince:   "30d",
	}

	result, _, err := handleReport(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleReport_Sections(t *testing.T) {
	dir := initTestRepo(t)

	input := ReportInput{
		Path:       dir,
		Collectors: "todos",
		Sections:   "todo-age",
	}

	result, _, err := handleReport(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
	assert.Contains(t, text, "todo-age")
}

func TestHandleReport_InvalidCollector(t *testing.T) {
	dir := initTestRepo(t)

	input := ReportInput{
		Path:       dir,
		Collectors: "nonexistent_collector",
	}

	_, _, err := handleReport(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline")
}

func TestHandleReport_SubdirectoryScan(t *testing.T) {
	dir := initTestRepo(t)

	subdir := filepath.Join(dir, "pkg", "sub")
	require.NoError(t, os.MkdirAll(subdir, 0o750))
	writeTestFile(t, dir, "pkg/sub/file.go", `package sub
// TODO: fix this
`)
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-m", "add subdir")

	input := ReportInput{
		Path:       subdir,
		Collectors: "todos",
	}

	result, _, err := handleReport(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleReport_DefaultCollectors(t *testing.T) {
	dir := initTestRepo(t)

	input := ReportInput{
		Path: dir,
	}

	result, _, err := handleReport(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleReport_ConfigLoadError(t *testing.T) {
	dir := initTestRepo(t)

	writeTestFile(t, dir, ".stringer.yaml", "invalid: [yaml: {broken")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-m", "add broken config")

	input := ReportInput{
		Path: dir,
	}

	_, _, err := handleReport(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestHandleContext_CustomWeeks(t *testing.T) {
	dir := initTestRepo(t)

	input := ContextInput{
		Path:  dir,
		Weeks: 8,
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleContext_InvalidPath(t *testing.T) {
	input := ContextInput{
		Path: "/nonexistent/path",
	}

	_, _, err := handleContext(context.Background(), nil, input)
	assert.Error(t, err)
}

func TestHandleDocs_Update(t *testing.T) {
	dir := initTestRepo(t)

	// Create an existing AGENTS.md for update mode.
	writeTestFile(t, dir, "AGENTS.md", `# AGENTS.md
<!-- GENERATED:START -->
old content
<!-- GENERATED:END -->

## Custom Section
My custom notes.
`)
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-m", "add agents.md")

	input := DocsInput{
		Path:   dir,
		Update: true,
	}

	result, _, err := handleDocs(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "AGENTS.md")
}

func TestHandleContext_NonGitDirectory(t *testing.T) {
	// A directory with no .git — AnalyzeHistory will fail (slog.Warn path)
	// and state.Load will return nil (no state file).
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeTestFile(t, dir, "go.mod", "module test\n\ngo 1.22\n")
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	input := ContextInput{
		Path: dir,
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleContext_CorruptedStateFile(t *testing.T) {
	dir := initTestRepo(t)

	// Create a corrupted state file to trigger state.Load error (slog.Warn path).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".stringer"), 0o750))
	writeTestFile(t, dir, ".stringer/last-scan.json", "not valid json{{{")

	input := ContextInput{
		Path: dir,
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleContext_MarkdownNonGitDirectory(t *testing.T) {
	// Test markdown format with no git history — covers the Generate branch
	// with nil history/state.
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	writeTestFile(t, dir, "go.mod", "module test\n\ngo 1.22\n")
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	input := ContextInput{
		Path:   dir,
		Format: "markdown",
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "CONTEXT.md")
}

func TestHandleDocs_UpdateMissingFile(t *testing.T) {
	dir := initTestRepo(t)

	// Update mode without an existing AGENTS.md — docs.Update should error.
	input := DocsInput{
		Path:   dir,
		Update: true,
	}

	_, _, err := handleDocs(context.Background(), nil, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{"", []string{}},
		{",,,", []string{}},
	}

	for _, tt := range tests {
		got := splitAndTrim(tt.input)
		assert.Equal(t, tt.expected, got, "input: %q", tt.input)
	}
}
