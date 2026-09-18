// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// lowerLotteryRiskThresholds lets tests with one-file fixtures exercise the
// ownership math without building substantial directories.
func lowerLotteryRiskThresholds(t *testing.T) {
	t.Helper()
	origFiles, origLines := minLotteryRiskFiles, minLotteryRiskLines
	minLotteryRiskFiles, minLotteryRiskLines = 1, 1
	t.Cleanup(func() {
		minLotteryRiskFiles, minLotteryRiskLines = origFiles, origLines
	})
}

// substantialGoFile returns a Go source file with the given number of lines,
// enough to clear minLotteryRiskLines when a few are combined.
func substantialGoFile(pkg string, lines int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", pkg)
	for i := 1; i < lines; i++ {
		fmt.Fprintf(&b, "func F%d() int { return %d }\n", i, i)
	}
	return b.String()
}

// substantialDir returns three 40-line Go files under dir.
func substantialDir(dir string) map[string]string {
	files := make(map[string]string)
	for i := 0; i < 3; i++ {
		files[filepath.Join(dir, fmt.Sprintf("f%d.go", i))] = substantialGoFile("p", 40)
	}
	return files
}

// --- Minimum substance ---

func TestHasMinimumSubstance(t *testing.T) {
	tests := []struct {
		name string
		own  *dirOwnership
		want bool
	}{
		{"enough files and lines", &dirOwnership{Path: "internal/x", SourceFiles: 3, TotalLines: 100}, true},
		{"too few files", &dirOwnership{Path: "internal/x", SourceFiles: 2, TotalLines: 500}, false},
		{"too few lines", &dirOwnership{Path: "internal/x", SourceFiles: 10, TotalLines: 99}, false},
		{"no source files", &dirOwnership{Path: "docs", SourceFiles: 0, TotalLines: 0}, false},
		{"fixtures dir", &dirOwnership{Path: "test/fixtures", SourceFiles: 10, TotalLines: 1000}, false},
		{"static dir", &dirOwnership{Path: "tests/static", SourceFiles: 10, TotalLines: 1000}, false},
		{"root", &dirOwnership{Path: ".", SourceFiles: 3, TotalLines: 100}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasMinimumSubstance(tt.own))
		})
	}
}

func TestIsNonCodeDir(t *testing.T) {
	assert.True(t, isNonCodeDir("tests/static"))
	assert.True(t, isNonCodeDir("test/fixtures"))
	assert.True(t, isNonCodeDir("tests/templates"))
	assert.True(t, isNonCodeDir("internal/testdata"))
	assert.True(t, isNonCodeDir("web/Assets"))
	assert.True(t, isNonCodeDir("src/__snapshots__"))
	assert.False(t, isNonCodeDir("internal/collectors"))
	assert.False(t, isNonCodeDir("test/support"))
	assert.False(t, isNonCodeDir("."))
	assert.False(t, isNonCodeDir("staticanalysis")) // segment must match exactly
}

func TestLotteryRiskCollector_SkipsTrivialAndStaticDirectories(t *testing.T) {
	// One substantial code directory plus a two-file dir, a fixtures dir full
	// of code, and a static dir holding only assets. Only the substantial
	// directory may be flagged.
	files := substantialDir("core")
	files["tiny/a.go"] = substantialGoFile("tiny", 60)
	files["tiny/b.go"] = substantialGoFile("tiny", 60)
	for k, v := range substantialDir("test/fixtures") {
		files[k] = v
	}
	files["tests/static/logo.svg"] = "<svg/>\n"
	files["tests/static/style.css"] = "body{}\n"
	files["tests/static/index.html"] = "<html></html>\n"
	_, dir := initGoGitRepo(t, files)

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var paths []string
	for _, sig := range filterByKind(signals, "low-lottery-risk") {
		paths = append(paths, sig.FilePath)
	}
	assert.Equal(t, []string{"core"}, paths)

	// Skipped directories are still present in metrics.
	metrics := c.Metrics().(*LotteryRiskMetrics)
	var metricPaths []string
	for _, d := range metrics.Directories {
		metricPaths = append(metricPaths, d.Path)
	}
	assert.Contains(t, metricPaths, "tiny")
	assert.Contains(t, metricPaths, "test/fixtures")
}

// --- Directory scoping ---

func TestLotteryRiskCollector_CommitWeightScopedPerDirectory(t *testing.T) {
	// alpha/ is written entirely by Alice; beta/ is split evenly between
	// Alice and Bob. If commit weight leaked repo-wide, both directories
	// would report the same percentages and beta would be flagged too.
	repo, dir := initGoGitRepo(t, map[string]string{"README.md": "# x\n"})
	now := time.Now()

	for i := 0; i < 3; i++ {
		addCommitAs(t, repo, dir, fmt.Sprintf("alpha/a%d.go", i), substantialGoFile("alpha", 40),
			"feat: alpha", now, "Alice", "alice@example.com")
	}
	addCommitAs(t, repo, dir, "beta/b0.go", substantialGoFile("beta", 40), "feat: beta", now, "Alice", "alice@example.com")
	addCommitAs(t, repo, dir, "beta/b1.go", substantialGoFile("beta", 40), "feat: beta", now, "Alice", "alice@example.com")
	addCommitAs(t, repo, dir, "beta/b2.go", substantialGoFile("beta", 40), "feat: beta", now, "Bob", "bob@example.com")
	addCommitAs(t, repo, dir, "beta/b3.go", substantialGoFile("beta", 40), "feat: beta", now, "Bob", "bob@example.com")

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	byPath := map[string]DirectoryOwnership{}
	for _, d := range c.Metrics().(*LotteryRiskMetrics).Directories {
		byPath[d.Path] = d
	}
	alpha, beta := byPath["alpha"], byPath["beta"]
	require.NotEmpty(t, alpha.Authors)
	require.NotEmpty(t, beta.Authors)

	assert.Equal(t, "Alice", alpha.Authors[0].Name)
	assert.InDelta(t, 1.0, alpha.Authors[0].Ownership, 0.001, "alpha is 100%% Alice")
	require.Len(t, beta.Authors, 2)
	assert.InDelta(t, 0.5, beta.Authors[0].Ownership, 0.001, "beta is split evenly, not inherited from alpha")
	assert.NotEqual(t, alpha.Authors[0].Ownership, beta.Authors[0].Ownership)

	var flagged []string
	for _, sig := range filterByKind(signals, "low-lottery-risk") {
		flagged = append(flagged, sig.FilePath)
		assert.Equal(t, 0.8, sig.Confidence, "full clone must not be capped")
		assert.NotContains(t, sig.Description, "History is shallow")
	}
	assert.Contains(t, flagged, "alpha")
	assert.NotContains(t, flagged, "beta")
}

func TestOwnershipFraction_SingleComponentRenormalized(t *testing.T) {
	assert.InDelta(t, 1.0, ownershipFraction(50, 50, 0, 0), 0.001, "blame-only sole author is 100%%")
	assert.InDelta(t, 1.0, ownershipFraction(0, 0, 3, 3), 0.001, "commit-only sole author is 100%%")
	assert.InDelta(t, 0.5, ownershipFraction(25, 50, 0, 0), 0.001)
	assert.InDelta(t, 0.6*0.5+0.4*1.0, ownershipFraction(25, 50, 3, 3), 0.001, "both components weighted")
	assert.InDelta(t, 0.0, ownershipFraction(0, 0, 0, 0), 0.001)
}

// --- Shallow history ---

func TestDetectHistory_FullClone(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{"main.go": "package main\n"})
	hist := detectHistory(context.Background(), dir)
	assert.False(t, hist.Shallow)
	assert.Equal(t, 1, hist.Commits)
}

func TestDetectHistory_NonGitDir(t *testing.T) {
	hist := detectHistory(context.Background(), t.TempDir())
	assert.False(t, hist.Shallow)
	assert.Equal(t, 0, hist.Commits)
}

func TestDetectHistory_MockError(t *testing.T) {
	origExecutor := testable.DefaultExecutor()
	gitcli.SetExecutor(&testable.MockCommandExecutor{DefaultError: "boom"})
	defer gitcli.SetExecutor(origExecutor)

	hist := detectHistory(context.Background(), t.TempDir())
	assert.Equal(t, historyInfo{}, hist)
}

func TestApplyShallowCaveat(t *testing.T) {
	sig := signal.RawSignal{Confidence: 0.8, Description: "Lottery risk: 1", Tags: []string{"low-lottery-risk"}}
	applyShallowCaveat(&sig, 100)
	assert.Equal(t, shallowConfidenceCap, sig.Confidence)
	assert.Contains(t, sig.Description, "History is shallow (100 commits)")
	assert.Contains(t, sig.Description, "Re-run on a full clone")
	assert.Contains(t, sig.Tags, "shallow-history")

	low := signal.RawSignal{Confidence: 0.3}
	applyShallowCaveat(&low, 1)
	assert.Equal(t, 0.3, low.Confidence, "cap never raises confidence")
}

func TestLotteryRiskCollector_ShallowCloneCappedAndAnnotated(t *testing.T) {
	// Alice writes all the code; Bob makes the most recent commit. Marking
	// Bob's commit as the shallow boundary makes blame attribute every line
	// to Bob, which is exactly the over-reporting the caveat describes.
	repo, dir := initGoGitRepo(t, substantialDir("core"))
	old := time.Now().Add(-48 * time.Hour)
	boundary := addCommitAs(t, repo, dir, "core/notes.go", "package p\n", "chore: touch", old, "Bob", "bob@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "shallow"), []byte(boundary.String()+"\n"), 0o600))

	hist := detectHistory(context.Background(), dir)
	require.True(t, hist.Shallow, "writing .git/shallow must mark the repo shallow")
	assert.Equal(t, 1, hist.Commits)

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	lottery := filterByKind(signals, "low-lottery-risk")
	require.NotEmpty(t, lottery)
	for _, sig := range lottery {
		assert.LessOrEqual(t, sig.Confidence, shallowConfidenceCap, "%s must be capped", sig.FilePath)
		assert.Contains(t, sig.Description, "History is shallow (1 commits)")
		assert.Contains(t, sig.Tags, "shallow-history")
		assert.Contains(t, sig.Title, "Bob", "shallow blame credits the boundary author")
	}
}
