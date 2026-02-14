// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package context

import (
	"bytes"
	"errors"
	"strings"
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

func TestRenderDirectoryTree_NestedDepth(t *testing.T) {
	entries := []docs.DirEntry{
		{Path: "cmd", IsDir: true, Depth: 1},
		{Path: "cmd/stringer", IsDir: true, Depth: 2},
		{Path: "internal", IsDir: true, Depth: 1},
	}

	result := renderDirectoryTree(entries, "myproject")
	assert.Contains(t, result, "myproject/")
	assert.Contains(t, result, "cmd/")
	assert.Contains(t, result, "│   ")
	assert.Contains(t, result, "stringer/")
	assert.Contains(t, result, "internal/")
}

func TestLastPathElement(t *testing.T) {
	assert.Equal(t, "file.go", lastPathElement("internal/output/file.go"))
	assert.Equal(t, "file.go", lastPathElement("file.go"))
	assert.Equal(t, "dir", lastPathElement("a/b/dir"))
}

func TestGenerate_WithMilestones(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	history := &GitHistory{
		TotalCommits: 10,
		RecentWeeks: []WeekActivity{
			{
				WeekStart: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
				Commits: []CommitSummary{
					{Hash: "abc12345", Message: "feat: release", Author: "alice", Tag: "v1.0.0"},
				},
				Tags: []TagInfo{
					{Name: "v1.0.0", Hash: "abc12345", Date: time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)},
				},
			},
		},
		TopAuthors: []AuthorStats{
			{Name: "alice", Commits: 10},
		},
		Milestones: []TagInfo{
			{Name: "v1.0.0", Hash: "abc12345", Date: time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)},
			{Name: "v0.9.0", Hash: "def67890", Date: time.Date(2026, 1, 28, 12, 0, 0, 0, time.UTC)},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, history, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "## Recent Milestones")
	assert.Contains(t, output, "**v1.0.0** — Feb 5, 2026 (`abc12345`)")
	assert.Contains(t, output, "**v0.9.0** — Jan 28, 2026 (`def67890`)")
}

func TestGenerate_NoMilestonesSection(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	history := &GitHistory{
		TotalCommits: 5,
		RecentWeeks: []WeekActivity{
			{
				WeekStart: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
				Commits: []CommitSummary{
					{Hash: "abc12345", Message: "feat: something", Author: "alice"},
				},
			},
		},
		TopAuthors: []AuthorStats{{Name: "alice", Commits: 5}},
	}

	var buf bytes.Buffer
	err := Generate(analysis, history, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "## Recent Milestones")
}

func TestGenerate_CommitIndicators(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	history := &GitHistory{
		TotalCommits: 4,
		RecentWeeks: []WeekActivity{
			{
				WeekStart: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
				Commits: []CommitSummary{
					{Hash: "aaa11111", Message: "feat: tagged release", Author: "alice", Tag: "v1.0.0"},
					{Hash: "bbb22222", Message: "feat: big change", Author: "bob", Files: 15},
					{Hash: "ccc33333", Message: "Merge branch feature", Author: "alice", IsMerge: true},
					{Hash: "ddd44444", Message: "fix: small fix", Author: "bob", Files: 2},
				},
				Tags: []TagInfo{
					{Name: "v1.0.0", Hash: "aaa11111", Date: time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)},
				},
			},
		},
		TopAuthors: []AuthorStats{
			{Name: "alice", Commits: 2},
			{Name: "bob", Commits: 2},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, history, nil, &buf)
	require.NoError(t, err)

	output := buf.String()

	// Tag indicator.
	assert.Contains(t, output, "[v1.0.0]")
	// Large file count indicator.
	assert.Contains(t, output, "[15 files]")
	// Merge indicator.
	assert.Contains(t, output, "[merge]")
	// Small fix should have no indicator — check it doesn't have brackets.
	assert.Contains(t, output, "`ddd44444` fix: small fix (bob)\n")
	// Week releases header.
	assert.Contains(t, output, "Releases: **v1.0.0**")
}

func TestCommitIndicators(t *testing.T) {
	tests := []struct {
		name   string
		commit CommitSummary
		want   string
	}{
		{
			name:   "no indicators",
			commit: CommitSummary{Files: 3},
			want:   "",
		},
		{
			name:   "tagged",
			commit: CommitSummary{Tag: "v1.0.0"},
			want:   "[v1.0.0]",
		},
		{
			name:   "many files",
			commit: CommitSummary{Files: 12},
			want:   "[12 files]",
		},
		{
			name:   "merge",
			commit: CommitSummary{IsMerge: true},
			want:   "[merge]",
		},
		{
			name:   "all indicators",
			commit: CommitSummary{Tag: "v2.0.0", Files: 20, IsMerge: true},
			want:   "[v2.0.0] [20 files] [merge]",
		},
		{
			name:   "exactly threshold",
			commit: CommitSummary{Files: 10},
			want:   "[10 files]",
		},
		{
			name:   "below threshold",
			commit: CommitSummary{Files: 9},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, commitIndicators(tt.commit))
		})
	}
}

func TestGenerate_WriteError(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	w := &errWriter{}
	err := Generate(analysis, nil, nil, w)
	require.Error(t, err)
}

// errWriter always returns an error on Write.
type errWriter struct{}

func (e *errWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write error")
}

func TestGenWriter_ErrorShortCircuits(t *testing.T) {
	// After the first write error, subsequent print/printf calls should be no-ops.
	w := &countWriter{failAfter: 1}
	g := &genWriter{w: w}

	g.print("first")             // succeeds
	g.printf("second %s", "val") // fails (2nd write)
	g.print("third")             // should be skipped
	g.printf("fourth %s", "val") // should be skipped

	require.Error(t, g.err)
	assert.Equal(t, 2, w.calls, "should have stopped writing after the error")
}

// countWriter counts writes and fails after failAfter successful calls.
type countWriter struct {
	failAfter int
	calls     int
}

func (cw *countWriter) Write(p []byte) (int, error) {
	cw.calls++
	if cw.calls > cw.failAfter {
		return 0, errors.New("write error")
	}
	return len(p), nil
}

func TestGenerate_MilestonesSectionOrder(t *testing.T) {
	// Milestones section should appear after Active Contributors and before Known Technical Debt.
	analysis := &docs.RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
	}

	history := &GitHistory{
		TotalCommits: 1,
		RecentWeeks: []WeekActivity{
			{
				WeekStart: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
				Commits:   []CommitSummary{{Hash: "abc12345", Message: "init", Author: "alice"}},
			},
		},
		TopAuthors: []AuthorStats{{Name: "alice", Commits: 1}},
		Milestones: []TagInfo{
			{Name: "v1.0.0", Hash: "abc12345", Date: time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)},
		},
	}

	var buf bytes.Buffer
	err := Generate(analysis, history, nil, &buf)
	require.NoError(t, err)

	output := buf.String()
	contributorsIdx := strings.Index(output, "## Active Contributors")
	milestonesIdx := strings.Index(output, "## Recent Milestones")
	debtIdx := strings.Index(output, "## Known Technical Debt")

	assert.Greater(t, milestonesIdx, contributorsIdx, "Milestones should come after Contributors")
	assert.Less(t, milestonesIdx, debtIdx, "Milestones should come before Technical Debt")
}
