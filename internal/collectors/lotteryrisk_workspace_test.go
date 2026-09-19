// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// authorNames returns the names of the authors credited for a directory.
func authorNames(dir DirectoryOwnership) []string {
	names := make([]string, 0, len(dir.Authors))
	for _, a := range dir.Authors {
		names = append(names, a.Name)
	}
	return names
}

// collectLotteryRiskMetrics runs the collector for one workspace of root and
// returns its directory metrics keyed by path.
func collectLotteryRiskMetrics(t *testing.T, root, rel string, members []string) map[string]DirectoryOwnership {
	t.Helper()
	opts := signal.CollectorOpts{WorkspaceMembers: members}
	repoPath := root
	if rel != "." {
		repoPath = filepath.Join(root, filepath.FromSlash(rel))
		opts.GitRoot = root
	}
	c := &LotteryRiskCollector{}
	_, err := c.Collect(context.Background(), repoPath, opts)
	require.NoError(t, err)
	m, ok := c.Metrics().(*LotteryRiskMetrics)
	require.True(t, ok)
	byPath := make(map[string]DirectoryOwnership, len(m.Directories))
	for _, d := range m.Directories {
		byPath[d.Path] = d
	}
	return byPath
}

func TestLotteryRiskCollector_WorkspacesScopeOwnership(t *testing.T) {
	lowerLotteryRiskThresholds(t)
	repo, root := initGoGitRepo(t, map[string]string{
		"cmd/main.go": "package main\n",
		"pkg/a/a.go":  "package a\n",
		"pkg/b/b.go":  "package b\n",
	})
	now := time.Now()
	addCommitAs(t, repo, root, "cmd/main.go", substantialGoFile("main", 30), "feat: cmd", now, "Carol", "carol@example.com")
	addCommitAs(t, repo, root, "pkg/a/a.go", substantialGoFile("a", 30), "feat: a", now, "Alice", "alice@example.com")
	addCommitAs(t, repo, root, "pkg/b/b.go", substantialGoFile("b", 30), "feat: b", now, "Bob", "bob@example.com")
	resetNumstatHistoryCache()
	t.Cleanup(resetNumstatHistoryCache)

	members := []string{".", "pkg/a", "pkg/b"}

	// The root workspace leaves member directories to their workspaces and
	// credits only its own files' authors.
	rootDirs := collectLotteryRiskMetrics(t, root, ".", members)
	assert.Contains(t, rootDirs, "cmd")
	assert.NotContains(t, rootDirs, "pkg/a")
	assert.NotContains(t, rootDirs, "pkg/b")
	assert.NotContains(t, rootDirs, "pkg", "a directory holding only member workspaces is empty")
	assert.Contains(t, authorNames(rootDirs["cmd"]), "Carol")
	assert.NotContains(t, authorNames(rootDirs["cmd"]), "Alice")
	assert.NotContains(t, authorNames(rootDirs["cmd"]), "Bob")

	// A member workspace blames its files from its own directory and
	// counts only commits that touched them.
	aDirs := collectLotteryRiskMetrics(t, root, "pkg/a", members)
	require.Contains(t, aDirs, ".")
	assert.Len(t, aDirs, 1)
	assert.Positive(t, aDirs["."].TotalLines, "blame resolves workspace-relative paths")
	assert.Contains(t, authorNames(aDirs["."]), "Alice")
	assert.NotContains(t, authorNames(aDirs["."]), "Bob")
	assert.NotContains(t, authorNames(aDirs["."]), "Carol")

	// The whole-repository view is unchanged: every directory, every author.
	allDirs := collectLotteryRiskMetrics(t, root, ".", nil)
	assert.Contains(t, allDirs, "pkg/a")
	assert.Contains(t, allDirs, "pkg/b")
	assert.Contains(t, authorNames(allDirs["pkg/b"]), "Bob")
}

func TestLotteryRiskCollector_WorkspaceSignalsScopedOnce(t *testing.T) {
	lowerLotteryRiskThresholds(t)
	repo, root := initGoGitRepo(t, map[string]string{
		"cmd/main.go": "package main\n",
		"pkg/a/a.go":  "package a\n",
	})
	now := time.Now()
	addCommitAs(t, repo, root, "cmd/main.go", substantialGoFile("main", 30), "feat: cmd", now, "Carol", "carol@example.com")
	addCommitAs(t, repo, root, "pkg/a/a.go", substantialGoFile("a", 30), "feat: a", now, "Alice", "alice@example.com")
	resetNumstatHistoryCache()
	t.Cleanup(resetNumstatHistoryCache)

	members := []string{".", "pkg/a"}
	var paths []string
	for _, rel := range members {
		opts := signal.CollectorOpts{WorkspaceMembers: members}
		repoPath := root
		if rel != "." {
			repoPath = filepath.Join(root, rel)
			opts.GitRoot = root
		}
		sigs, err := (&LotteryRiskCollector{}).Collect(context.Background(), repoPath, opts)
		require.NoError(t, err)
		for _, s := range filterByKind(sigs, "low-lottery-risk") {
			p := s.FilePath
			if rel != "." {
				p = path.Join(rel, p) // as stampWorkspace does
			}
			paths = append(paths, p)
		}
	}
	assert.ElementsMatch(t, []string{"cmd", "pkg/a"}, paths, "each directory is reported by exactly one workspace")
}
