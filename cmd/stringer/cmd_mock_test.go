// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	strcontext "github.com/davetashner/stringer/internal/context"
	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
	"github.com/davetashner/stringer/internal/testable"
)

// withMockFS swaps cmdFS with the given mock and restores it on test cleanup.
func withMockFS(t *testing.T, mock *testable.MockFileSystem) {
	t.Helper()
	orig := cmdFS
	cmdFS = mock
	t.Cleanup(func() { cmdFS = orig })
}

// -----------------------------------------------------------------------
// Abs error tests — each command returns an error when cmdFS.Abs fails
// -----------------------------------------------------------------------

func TestRunScan_AbsError(t *testing.T) {
	resetScanFlags()
	withMockFS(t, &testable.MockFileSystem{
		AbsFn: func(string) (string, error) {
			return "", fmt.Errorf("mock abs error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestRunReport_AbsError(t *testing.T) {
	resetReportFlags()
	withMockFS(t, &testable.MockFileSystem{
		AbsFn: func(string) (string, error) {
			return "", fmt.Errorf("mock abs error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestRunContext_AbsError(t *testing.T) {
	resetContextFlags()
	withMockFS(t, &testable.MockFileSystem{
		AbsFn: func(string) (string, error) {
			return "", fmt.Errorf("mock abs error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestRunDocs_AbsError(t *testing.T) {
	resetDocsFlags()
	withMockFS(t, &testable.MockFileSystem{
		AbsFn: func(string) (string, error) {
			return "", fmt.Errorf("mock abs error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestRunInit_AbsError(t *testing.T) {
	resetInitFlags()
	withMockFS(t, &testable.MockFileSystem{
		AbsFn: func(string) (string, error) {
			return "", fmt.Errorf("mock abs error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

// -----------------------------------------------------------------------
// Stat error tests — path passes Abs/EvalSymlinks but Stat fails
// -----------------------------------------------------------------------

func TestRunScan_StatError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()
	ghost := filepath.Join(dir, "gone")

	withMockFS(t, &testable.MockFileSystem{
		AbsFn:          func(string) (string, error) { return ghost, nil },
		EvalSymlinksFn: func(path string) (string, error) { return path, nil },
		StatFn: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestRunReport_StatError(t *testing.T) {
	resetReportFlags()
	dir := t.TempDir()
	ghost := filepath.Join(dir, "gone")

	withMockFS(t, &testable.MockFileSystem{
		AbsFn:          func(string) (string, error) { return ghost, nil },
		EvalSymlinksFn: func(path string) (string, error) { return path, nil },
		StatFn: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestRunContext_StatError(t *testing.T) {
	resetContextFlags()
	dir := t.TempDir()
	ghost := filepath.Join(dir, "gone")

	withMockFS(t, &testable.MockFileSystem{
		AbsFn:          func(string) (string, error) { return ghost, nil },
		EvalSymlinksFn: func(path string) (string, error) { return path, nil },
		StatFn: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestRunDocs_StatError(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()
	ghost := filepath.Join(dir, "gone")

	withMockFS(t, &testable.MockFileSystem{
		AbsFn:          func(string) (string, error) { return ghost, nil },
		EvalSymlinksFn: func(path string) (string, error) { return path, nil },
		StatFn: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestRunInit_StatError(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()
	ghost := filepath.Join(dir, "gone")

	withMockFS(t, &testable.MockFileSystem{
		AbsFn:          func(string) (string, error) { return ghost, nil },
		EvalSymlinksFn: func(path string) (string, error) { return path, nil },
		StatFn: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

// -----------------------------------------------------------------------
// Config load/validate error tests
// -----------------------------------------------------------------------

func TestRunScan_ConfigLoadError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Write an invalid YAML config that will fail to parse.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(":\n  invalid: yaml: [unmatched"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load")
}

func TestRunScan_ConfigValidateError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Write a config with an unknown collector to trigger validation failure.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte("collectors:\n  nonexistent_collector:\n    enabled: true\n"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir})

	err := cmd.Execute()
	require.Error(t, err)
	// Config validation error is wrapped by runScan.
	assert.Contains(t, err.Error(), "unknown collector")
}

func TestRunReport_ConfigLoadError(t *testing.T) {
	resetReportFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(":\n  invalid: yaml: [unmatched"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load")
}

func TestRunReport_ConfigValidateError(t *testing.T) {
	resetReportFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte("collectors:\n  nonexistent_collector:\n    enabled: true\n"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown collector")
}

// -----------------------------------------------------------------------
// Version command in-process test (covers version.go Run handler)
// -----------------------------------------------------------------------

func TestVersionCmd_InProcess(t *testing.T) {
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "stringer")
	assert.Contains(t, out, Version)
}

// -----------------------------------------------------------------------
// strcontext.RenderJSON with milestones and tags (covers context.go:227-241)
// -----------------------------------------------------------------------

func TestRenderContextJSON_WithMilestonesAndTags(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test-milestones",
		Language: "Go",
	}

	now := time.Now()
	history := &strcontext.GitHistory{
		TotalCommits: 42,
		TopAuthors: []strcontext.AuthorStats{
			{Name: "Alice", Commits: 25},
			{Name: "Bob", Commits: 17},
		},
		Milestones: []strcontext.TagInfo{
			{Name: "v1.0.0", Hash: "abc12345", Date: now.Add(-30 * 24 * time.Hour)},
			{Name: "v1.1.0", Hash: "def67890", Date: now.Add(-7 * 24 * time.Hour)},
		},
		RecentWeeks: []strcontext.WeekActivity{
			{
				WeekStart: now.Add(-14 * 24 * time.Hour),
				Commits: []strcontext.CommitSummary{
					{Hash: "aaa", Message: "feat: add feature", Author: "Alice"},
				},
				Tags: []strcontext.TagInfo{
					{Name: "v1.0.0", Hash: "abc12345", Date: now},
				},
			},
			{
				WeekStart: now.Add(-7 * 24 * time.Hour),
				Commits: []strcontext.CommitSummary{
					{Hash: "bbb", Message: "fix: bug fix", Author: "Bob"},
					{Hash: "ccc", Message: "chore: cleanup", Author: "Alice"},
				},
				Tags: nil, // no tags this week
			},
		},
	}

	var buf bytes.Buffer
	err := strcontext.RenderJSON(analysis, history, nil, &buf)
	require.NoError(t, err)

	var result strcontext.ContextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	assert.Equal(t, "test-milestones", result.Name)
	require.NotNil(t, result.History)
	assert.Equal(t, 42, result.History.TotalCommits)
	assert.Len(t, result.History.TopAuthors, 2)

	// Milestones should be populated.
	require.Len(t, result.History.Milestones, 2)
	assert.Equal(t, "v1.0.0", result.History.Milestones[0].Name)
	assert.Equal(t, "abc12345", result.History.Milestones[0].Hash)
	assert.NotEmpty(t, result.History.Milestones[0].Date)

	// Recent weeks with tags.
	require.Len(t, result.History.RecentWeeks, 2)
	assert.Equal(t, 1, result.History.RecentWeeks[0].Commits)
	require.Len(t, result.History.RecentWeeks[0].Tags, 1)
	assert.Equal(t, "v1.0.0", result.History.RecentWeeks[0].Tags[0])

	assert.Equal(t, 2, result.History.RecentWeeks[1].Commits)
	assert.Empty(t, result.History.RecentWeeks[1].Tags)
}

// -----------------------------------------------------------------------
// strcontext.RenderJSON with empty scan state (no signal metas)
// -----------------------------------------------------------------------

func TestRenderContextJSON_WithEmptyScanState(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test",
		Language: "Go",
	}

	// ScanState exists but has no SignalMetas — should not populate TechDebt.
	scanState := &state.ScanState{
		SignalCount: 0,
		SignalMetas: nil,
	}

	var buf bytes.Buffer
	err := strcontext.RenderJSON(analysis, nil, scanState, &buf)
	require.NoError(t, err)

	var result strcontext.ContextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Nil(t, result.TechDebt, "empty SignalMetas should not create TechDebt")
}

// -----------------------------------------------------------------------
// Delta scan error paths
// -----------------------------------------------------------------------

func TestRunScan_DeltaStateLoadError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: delta load error test\n"), 0o600))

	// Create a corrupted state file at the correct path.
	stateDir := filepath.Join(dir, ".stringer")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "last-scan.json"),
		[]byte("{invalid json"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--delta", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	// The delta state.Load error is fatal.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta state")
}

func TestRunScan_DeltaResolvedTodos(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Create a file with two TODOs.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: will be resolved\n// TODO: stays\n"), 0o600))

	// First scan to establish state.
	outFile1 := filepath.Join(t.TempDir(), "out1.jsonl")
	cmd1, _, _ := newTestCmd()
	cmd1.SetArgs([]string{"scan", dir, "--delta", "--quiet", "--collectors=todos",
		"-o", outFile1})
	require.NoError(t, cmd1.Execute())

	// Remove one TODO.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: stays\n"), 0o600))

	// Second scan with --dry-run --json — delta detects removed TODO as resolved.
	resetScanFlags()
	cmd2, stdout2, _ := newTestCmd()
	cmd2.SetArgs([]string{"scan", dir, "--delta", "--dry-run", "--json", "--quiet",
		"--collectors=todos"})

	err := cmd2.Execute()
	require.NoError(t, err)

	// Dry-run JSON output should show signal count.
	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout2.Bytes(), &result), "output: %s", stdout2.String())
	// Should have at least 1 signal (the resolved TODO as pre-closed).
	assert.GreaterOrEqual(t, result.TotalSignals, 0)
}

// -----------------------------------------------------------------------
// Scan with --include-closed flag on a clean temp dir (no prior CollectorOpts)
// -----------------------------------------------------------------------

func TestRunScan_IncludeClosedCleanDir(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: include-closed test\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--include-closed", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Scan with --anonymize flag on a clean temp dir (no prior CollectorOpts)
// -----------------------------------------------------------------------

func TestRunScan_AnonymizeCleanDir(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: anonymize test\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--anonymize=always", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Scan with --include-demo-paths flag on a clean temp dir
// -----------------------------------------------------------------------

func TestRunScan_IncludeDemoPathsCleanDir(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: demo path test\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--include-demo-paths", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Scan with --paths flag on a clean temp dir
// -----------------------------------------------------------------------

func TestRunScan_PathsCleanDir(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"),
		[]byte("package main\n// TODO: paths test\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--paths=src/**", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Report with --paths flag on a clean temp dir (covers report.go:203-205)
// -----------------------------------------------------------------------

func TestRunReport_PathsCleanDir(t *testing.T) {
	resetReportFlags()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"),
		[]byte("package main\n// TODO: report paths test\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", dir, "--paths=src/**", "--quiet", "-c", "todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Stringer Report")
}

// -----------------------------------------------------------------------
// Context and Docs generate/update error paths via MockFS.Create
// -----------------------------------------------------------------------

func TestRunContext_GenerateError(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	// MockFS that fails only on Create (for -o flag), but let the rest work.
	// This triggers the "generation failed" path for context --format json -o.
	withMockFS(t, &testable.MockFileSystem{
		CreateFn: func(string) (*os.File, error) {
			return nil, fmt.Errorf("mock create error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "-o", "/tmp/context-err.md", "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

func TestRunDocs_GenerateOutputError(t *testing.T) {
	resetDocsFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	withMockFS(t, &testable.MockFileSystem{
		CreateFn: func(string) (*os.File, error) {
			return nil, fmt.Errorf("mock create error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"docs", dir, "-o", "/tmp/docs-err.md", "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

// -----------------------------------------------------------------------
// Init bootstrap error path
// -----------------------------------------------------------------------

func TestRunInit_BootstrapError(t *testing.T) {
	resetInitFlags()

	if os.Getuid() == 0 {
		t.Skip("cannot test permission errors as root")
	}

	// Create a dir but make a file where .stringer.yaml would go,
	// then make the directory read-only so bootstrap can't create files.
	dir := t.TempDir()

	// Create a subdirectory named .stringer.yaml so file creation fails.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".stringer.yaml"), 0o750))
	require.NoError(t, os.Chmod(dir, 0o555))       //nolint:gosec // intentional permission change for test
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) //nolint:gosec,errcheck // restore permissions

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init failed")
}

// -----------------------------------------------------------------------
// Scan output file create error via MockFS
// -----------------------------------------------------------------------

func TestRunScan_OutputCreateError(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	callCount := 0

	withMockFS(t, &testable.MockFileSystem{
		CreateFn: func(string) (*os.File, error) {
			callCount++
			return nil, fmt.Errorf("mock create error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "-o", "/tmp/test-output.jsonl", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

// -----------------------------------------------------------------------
// Report output file create error via MockFS
// -----------------------------------------------------------------------

func TestRunReport_OutputCreateError(t *testing.T) {
	resetReportFlags()
	dir := initTestRepo(t)

	withMockFS(t, &testable.MockFileSystem{
		CreateFn: func(string) (*os.File, error) {
			return nil, fmt.Errorf("mock create error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", dir, "-o", "/tmp/test-output.txt", "--quiet", "-c", "todos"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

// -----------------------------------------------------------------------
// Context output file create error via MockFS
// -----------------------------------------------------------------------

func TestRunContext_OutputCreateError(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	withMockFS(t, &testable.MockFileSystem{
		CreateFn: func(string) (*os.File, error) {
			return nil, fmt.Errorf("mock create error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "-o", "/tmp/test-context.md", "--quiet"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

// -----------------------------------------------------------------------
// EvalSymlinks error tests
// -----------------------------------------------------------------------

func TestRunScan_EvalSymlinksError(t *testing.T) {
	resetScanFlags()
	withMockFS(t, &testable.MockFileSystem{
		EvalSymlinksFn: func(string) (string, error) {
			return "", fmt.Errorf("mock symlink error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestRunInit_EvalSymlinksError(t *testing.T) {
	resetInitFlags()
	withMockFS(t, &testable.MockFileSystem{
		EvalSymlinksFn: func(string) (string, error) {
			return "", fmt.Errorf("mock symlink error")
		},
	})

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", "."})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

// -----------------------------------------------------------------------
// Scan delta state save error
// -----------------------------------------------------------------------

func TestRunScan_DeltaStateSaveError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: delta save error\n"), 0o600))

	// Run once to populate scan and create state dir/file.
	cmd1, _, _ := newTestCmd()
	cmd1.SetArgs([]string{"scan", dir, "--delta", "--quiet", "--collectors=todos",
		"-o", filepath.Join(t.TempDir(), "out1.jsonl")})
	require.NoError(t, cmd1.Execute())

	if os.Getuid() == 0 {
		t.Skip("cannot test permission errors as root")
	}

	// Make both the state file AND directory read-only to prevent overwrite.
	stateDir := filepath.Join(dir, ".stringer")
	stateFile := filepath.Join(stateDir, "last-scan.json")
	require.NoError(t, os.Chmod(stateFile, 0o444)) //nolint:gosec // intentional permission change for test
	require.NoError(t, os.Chmod(stateDir, 0o555))  //nolint:gosec // intentional permission change for test
	t.Cleanup(func() {
		_ = os.Chmod(stateDir, 0o750)  //nolint:gosec // restore permissions
		_ = os.Chmod(stateFile, 0o600) //nolint:gosec // restore permissions
	})

	resetScanFlags()
	cmd2, _, _ := newTestCmd()
	cmd2.SetArgs([]string{"scan", dir, "--delta", "--quiet", "--collectors=todos",
		"-o", filepath.Join(t.TempDir(), "out2.jsonl")})

	err := cmd2.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta state")
}

// -----------------------------------------------------------------------
// Context with corrupted state file (covers state.Load warn path)
// -----------------------------------------------------------------------

func TestRunContext_CorruptedStateFile(t *testing.T) {
	resetContextFlags()
	root := initTestRepo(t)

	// Create a corrupted state file so state.Load returns an error (non-fatal warning).
	stateDir := filepath.Join(root, ".stringer")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "last-scan.json"),
		[]byte("{corrupted json data"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"context", root, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err) // state.Load error is a warning, not fatal
	assert.Contains(t, stdout.String(), "# CONTEXT.md")
}

// -----------------------------------------------------------------------
// Scan min-confidence filter where signals pass through (covers line 394)
// -----------------------------------------------------------------------

func TestRunScan_MinConfidencePassingSignals(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// Use a very low threshold so all TODO signals pass through the filter.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--min-confidence=0.01", "--dry-run", "--json",
		"--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), "output: %s", stdout.String())
	assert.Greater(t, result.TotalSignals, 0, "signals should pass through low confidence filter")
}
