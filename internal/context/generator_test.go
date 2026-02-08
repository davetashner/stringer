package context

import (
	"bytes"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_BasicOutput(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
		TechStack: []docs.TechComponent{
			{Name: "Go", Version: "1.24"},
			{Name: "Cobra"},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, nil, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "# CONTEXT.md — myproject")
	assert.Contains(t, output, "**Primary Language**: Go")
	assert.Contains(t, output, "Go 1.24, Cobra")
	assert.Contains(t, output, "No scan data available")
}

func TestGenerate_WithBuildCommands(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
		BuildCommands: []docs.BuildCommand{
			{Name: "build", Command: "go build ./..."},
			{Name: "test", Command: "go test ./..."},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, nil, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "go build ./...")
	assert.Contains(t, output, "go test ./...")
}

func TestGenerate_WithGitHistory(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	history := &GitHistory{
		TotalCommits: 42,
		RecentWeeks: []WeekActivity{
			{
				WeekStart: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
				Commits: []CommitSummary{
					{Hash: "abc12345", Message: "feat: add feature", Author: "alice", Date: time.Now()},
					{Hash: "def67890", Message: "fix: bug fix", Author: "bob", Date: time.Now()},
				},
			},
		},
		TopAuthors: []AuthorStats{
			{Name: "alice", Commits: 25},
			{Name: "bob", Commits: 17},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, history, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "42 commits total")
	assert.Contains(t, output, "Week of Feb 2, 2026")
	assert.Contains(t, output, "feat: add feature")
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "bob")
	assert.Contains(t, output, "Active Contributors")
	assert.Contains(t, output, "**alice**: 25 commits")
}

func TestGenerate_WithScanState(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	scanState := &state.ScanState{
		SignalCount: 5,
		SignalMetas: []state.SignalMeta{
			{Kind: "todo", Title: "Add tests", FilePath: "main.go", Line: 10},
			{Kind: "todo", Title: "Refactor handler", FilePath: "handler.go", Line: 20},
			{Kind: "fixme", Title: "Fix race condition", FilePath: "worker.go", Line: 5},
			{Kind: "churn", Title: "high churn in config.go", FilePath: "config.go"},
			{Kind: "hack", Title: "temporary workaround", FilePath: "auth.go", Line: 100},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, nil, scanState, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "5 signals from last scan")
	assert.Contains(t, output, "TODOs (2)")
	assert.Contains(t, output, "FIXMEs (1)")
	assert.Contains(t, output, "Churn Hotspots (1)")
	assert.Contains(t, output, "Hacks (1)")
	assert.Contains(t, output, "Add tests")
	assert.Contains(t, output, "`main.go:10`")
}

func TestGenerate_ScanStateManySignals(t *testing.T) {
	analysis := &docs.RepoAnalysis{Name: "proj"}

	metas := make([]state.SignalMeta, 8)
	for i := range metas {
		metas[i] = state.SignalMeta{Kind: "todo", Title: "Task " + string(rune('A'+i)), FilePath: "f.go"}
	}

	scanState := &state.ScanState{
		SignalCount: 8,
		SignalMetas: metas,
	}

	var buf bytes.Buffer
	err := Generate(analysis, nil, scanState, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "TODOs (8)")
	assert.Contains(t, output, "and 3 more")
}

func TestGenerate_WithDirectoryTree(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name: "myproject",
		DirectoryTree: []docs.DirEntry{
			{Path: "cmd", IsDir: true, Depth: 1},
			{Path: "internal", IsDir: true, Depth: 1},
			{Path: "go.mod", IsDir: false, Depth: 1},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, nil, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "## Architecture")
	assert.Contains(t, output, "cmd/")
	assert.Contains(t, output, "internal/")
	assert.Contains(t, output, "go.mod")
}

func TestGenerate_WithPatterns(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name: "myproject",
		Patterns: []docs.CodePattern{
			{Name: "Cobra CLI", Description: "Uses spf13/cobra for CLI"},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, nil, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Key Patterns")
	assert.Contains(t, output, "**Cobra CLI**")
}

func TestFormatKindLabel(t *testing.T) {
	assert.Equal(t, "TODOs", formatKindLabel("todo"))
	assert.Equal(t, "FIXMEs", formatKindLabel("fixme"))
	assert.Equal(t, "Hacks", formatKindLabel("hack"))
	assert.Equal(t, "Hacks", formatKindLabel("xxx"))
	assert.Equal(t, "Bugs", formatKindLabel("bug"))
	assert.Equal(t, "Churn Hotspots", formatKindLabel("churn"))
	assert.Equal(t, "Large Files", formatKindLabel("large_file"))
	assert.Equal(t, "Lottery Risk", formatKindLabel("lottery_risk"))
	assert.Equal(t, "Revert", formatKindLabel("revert"))
}

func TestRenderDirectoryTree(t *testing.T) {
	entries := []docs.DirEntry{
		{Path: "cmd", IsDir: true, Depth: 1},
		{Path: "internal", IsDir: true, Depth: 1},
	}

	result := renderDirectoryTree(entries, "myproject")
	assert.Contains(t, result, "myproject/")
	assert.Contains(t, result, "cmd/")
	assert.Contains(t, result, "internal/")
}

func TestRenderDirectoryTree_Empty(t *testing.T) {
	result := renderDirectoryTree(nil, "proj")
	assert.Equal(t, "proj/\n", result)
}

func TestLastPathElement(t *testing.T) {
	assert.Equal(t, "file.go", lastPathElement("internal/output/file.go"))
	assert.Equal(t, "file.go", lastPathElement("file.go"))
	assert.Equal(t, "dir", lastPathElement("a/b/dir"))
}

func TestSortStrings(t *testing.T) {
	s := []string{"c", "a", "b"}
	sortStrings(s)
	assert.Equal(t, []string{"a", "b", "c"}, s)
}
